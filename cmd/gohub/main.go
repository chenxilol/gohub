package main

import (
	"context"
	"errors"
	"flag"
	"gohub/internal/auth"
	"gohub/internal/bus"
	hub_nats "gohub/internal/bus/nats"
	"gohub/internal/dispatcher"
	"gohub/internal/handlers"
	hub2 "gohub/internal/hub"
	"gohub/internal/metrics"
	"gohub/internal/sdk"
	myws "gohub/internal/websocket"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/viper"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// 命令行参数
var (
	configFile = flag.String("config", "configs/config.yaml", "配置文件路径")
)

// WebSocket升级器
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	flag.Parse()

	// 加载配置
	config, err := loadConfig(*configFile)
	if err != nil {
		// 使用简单日志记录配置加载错误
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// 初始化日志
	logLevel := parseLogLevel(config.Log.Level)
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	logger := slog.New(logHandler)
	slog.SetDefault(logger)
	slog.Info("Logger initialized", "level", config.Log.Level)

	// 初始化默认指标
	metrics.Default()

	var messageBus bus.MessageBus
	if config.Cluster.Enabled {
		slog.Info("Cluster mode enabled, using message bus")
		// 从配置创建NATS连接
		natsConfig := hub_nats.Config{
			URLs:             config.Cluster.NATS.URLs,
			Name:             config.Cluster.NATS.Name,
			ReconnectWait:    config.Cluster.NATS.ReconnectWait,
			MaxReconnects:    config.Cluster.NATS.MaxReconnects,
			ConnectTimeout:   config.Cluster.NATS.ConnectTimeout,
			OpTimeout:        config.Cluster.NATS.OpTimeout,
			UseJetStream:     config.Cluster.NATS.UseJetStream,
			StreamName:       config.Cluster.NATS.StreamName,
			ConsumerName:     config.Cluster.NATS.ConsumerName,
			MessageRetention: config.Cluster.NATS.MessageRetention,
		}
		messageBus, err = hub_nats.New(natsConfig)
		if err != nil {
			slog.Error("Failed to connect to NATS", "error", err)
			os.Exit(1)
		}
	}

	// 创建认证服务
	authService := auth.NewJWTService(config.Auth.SecretKey, config.Auth.Issuer)

	// 创建Hub
	hubConfig := hub2.Config{
		ReadTimeout:      config.Server.ReadTimeout,
		WriteTimeout:     config.Server.WriteTimeout,
		ReadBufferSize:   config.Server.ReadBufferSize,
		WriteBufferSize:  config.Server.WriteBufferSize,
		MessageBufferCap: config.Server.MessageBufferCap,
		BusTimeout:       5 * time.Second,
	}
	wsHub := hub2.NewHub(messageBus, hubConfig)
	defer wsHub.Close()

	// 创建SDK
	gohubSDK := sdk.NewSDK(wsHub)

	// 注册消息处理函数
	d := dispatcher.GetDispatcher()
	handlers.RegisterHandlers(d)

	// 添加Prometheus指标接口
	http.Handle("/metrics", promhttp.HandlerFor(
		metrics.GetRegistry(),
		promhttp.HandlerOpts{},
	))

	// HTTP处理函数：WebSocket连接入口
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// 获取并验证认证令牌
		token := r.URL.Query().Get("token")
		if token == "" {
			token = r.Header.Get("Authorization")
			// 移除Bearer前缀（如果存在）
			if strings.HasPrefix(token, "Bearer ") {
				token = token[7:]
			}
		}

		// 验证令牌
		var claims *auth.TokenClaims
		var authErr error
		if token != "" {
			claims, authErr = authService.Authenticate(r.Context(), token)
			if authErr != nil {
				slog.Error("Authentication failed", "error", authErr)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				metrics.RecordAuthFailure()
				return
			}
			metrics.RecordAuthSuccess()
		} else if !config.Auth.AllowAnonymous {
			// 如果不允许匿名访问且没有提供令牌，则拒绝连接
			slog.Warn("WebSocket connection attempt without token")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			metrics.RecordAuthFailure()
			return
		}

		// 升级连接为WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("Failed to upgrade connection", "error", err)
			metrics.RecordError()
			return
		}

		// 获取客户端ID
		var clientID string
		if claims != nil {
			// 如果有认证信息，使用用户ID作为客户端ID
			clientID = claims.UserID
		} else {
			// 否则从查询参数获取，或生成一个
			clientID = r.URL.Query().Get("client_id")
			if clientID == "" {
				clientID = generateClientID()
			}
		}

		// 将Hub实例添加到上下文中
		requestCtxWithHub := context.WithValue(r.Context(), "hub", wsHub)

		// 创建WebSocket客户端
		wsConn := myws.NewGorillaConn(conn)
		client := hub2.NewClient(requestCtxWithHub, clientID, wsConn, hubConfig, func(id string) {
			wsHub.Unregister(id)
			metrics.ClientDisconnected()
		}, d)

		// 如果有认证声明，保存到客户端
		if claims != nil {
			client.SetAuthClaims(claims)
		}

		// 注册到Hub
		wsHub.Register(client)
		metrics.ClientConnected()

		// 触发SDK客户端连接事件
		go gohubSDK.TriggerEvent(requestCtxWithHub, sdk.Event{
			Type:     sdk.EventClientConnected,
			ClientID: clientID,
			Time:     time.Now(),
			Claims:   claims,
		})

		slog.Info("New WebSocket connection established",
			"client_id", clientID,
			"authenticated", claims != nil,
			"username", func() string {
				if claims != nil {
					return claims.Username
				}
				return "anonymous"
			}())
	})

	// 健康检查端点
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","version":"` + config.Version + `"}`))
	})

	// 启动HTTP服务器
	srv := &http.Server{
		Addr:    config.Server.Addr,
		Handler: nil, // 使用默认的ServeMux
	}

	// 优雅关闭
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		// 监听系统信号
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		slog.Info("Shutting down server...")

		// 给现有连接5秒时间关闭
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("Server shutdown error", "error", err)
		}
	}()

	// 启动服务器
	slog.Info("Starting server", "addr", config.Server.Addr, "cluster_mode", config.Cluster.Enabled)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		slog.Error("HTTP server error", "error", err)
	}

	// 等待优雅关闭完成
	wg.Wait()
	slog.Info("Server stopped")
}

// 配置结构
type Config struct {
	Server struct {
		Addr             string        `mapstructure:"addr"`
		ReadTimeout      time.Duration `mapstructure:"read_timeout"`
		WriteTimeout     time.Duration `mapstructure:"write_timeout"`
		ReadBufferSize   int           `mapstructure:"read_buffer_size"`
		WriteBufferSize  int           `mapstructure:"write_buffer_size"`
		MessageBufferCap int           `mapstructure:"message_buffer_cap"`
	} `mapstructure:"server"`

	Cluster struct {
		Enabled bool `mapstructure:"enabled"`
		Etcd    struct {
			Endpoints   []string      `mapstructure:"endpoints"`
			DialTimeout time.Duration `mapstructure:"dial_timeout"`
			KeyPrefix   string        `mapstructure:"key_prefix"`
		} `mapstructure:"etcd"`
		// 添加NATS配置
		NATS struct {
			URLs             []string      `mapstructure:"urls"`
			Name             string        `mapstructure:"name"`
			ReconnectWait    time.Duration `mapstructure:"reconnect_wait"`
			MaxReconnects    int           `mapstructure:"max_reconnects"`
			ConnectTimeout   time.Duration `mapstructure:"connect_timeout"`
			OpTimeout        time.Duration `mapstructure:"op_timeout"`
			UseJetStream     bool          `mapstructure:"use_jetstream"`
			StreamName       string        `mapstructure:"stream_name"`
			ConsumerName     string        `mapstructure:"consumer_name"`
			MessageRetention time.Duration `mapstructure:"message_retention"`
		} `mapstructure:"nats"`
	} `mapstructure:"cluster"`

	Auth struct {
		Enabled        bool   `mapstructure:"enabled"`
		SecretKey      string `mapstructure:"secret_key"`
		Issuer         string `mapstructure:"issuer"`
		AllowAnonymous bool   `mapstructure:"allow_anonymous"`
	} `mapstructure:"auth"`

	// 添加日志配置
	Log struct {
		Level string `mapstructure:"level"`
	} `mapstructure:"log"`

	// 添加版本字段
	Version string `mapstructure:"version"`
}

// 加载配置
func loadConfig(configFile string) (Config, error) {
	viper.SetConfigFile(configFile)

	// 设置默认值
	viper.SetDefault("server.addr", ":8080")
	viper.SetDefault("server.read_timeout", 60*time.Second)
	viper.SetDefault("server.write_timeout", 60*time.Second)
	viper.SetDefault("server.read_buffer_size", 4*1024)
	viper.SetDefault("server.write_buffer_size", 4*1024)
	viper.SetDefault("server.message_buffer_cap", 256)

	viper.SetDefault("cluster.enabled", false)

	// NATS默认配置
	viper.SetDefault("cluster.nats.urls", []string{"nats://localhost:4222"})
	viper.SetDefault("cluster.nats.name", "gohub-client")
	viper.SetDefault("cluster.nats.reconnect_wait", 2*time.Second)
	viper.SetDefault("cluster.nats.max_reconnects", -1)
	viper.SetDefault("cluster.nats.connect_timeout", 5*time.Second)
	viper.SetDefault("cluster.nats.op_timeout", 10*time.Millisecond)
	viper.SetDefault("cluster.nats.use_jetstream", false)
	viper.SetDefault("cluster.nats.stream_name", "GOHUB")
	viper.SetDefault("cluster.nats.consumer_name", "gohub-consumer")
	viper.SetDefault("cluster.nats.message_retention", 1*time.Hour)

	viper.SetDefault("auth.enabled", false)
	viper.SetDefault("auth.secret_key", "changeme")
	viper.SetDefault("auth.issuer", "gohub")
	viper.SetDefault("auth.allow_anonymous", true)

	// 日志默认配置
	viper.SetDefault("log.level", "info")

	viper.SetDefault("version", "dev")

	// 支持环境变量
	viper.SetEnvPrefix("GOHUB")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return Config{}, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return Config{}, err
	}

	return config, nil
}

// 生成客户端ID
func generateClientID() string {
	return uuid.New().String()
}

// 解析日志级别
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
