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

	// 初始化日志
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	config, err := loadConfig(*configFile)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 初始化默认指标
	metrics.Default()

	var messageBus bus.MessageBus
	if config.Cluster.Enabled {
		slog.Info("Cluster mode enabled, using message bus")
		// 使用 noop 作为默认消息总线 (将来可替换为 Redis/NATS)
		messageBus, err = hub_nats.New(hub_nats.DefaultConfig())
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

		// 创建WebSocket客户端
		wsConn := myws.NewGorillaConn(conn)
		client := hub2.NewClient(ctx, clientID, wsConn, hubConfig, func(id string) {
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
		go gohubSDK.TriggerEvent(ctx, sdk.Event{
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

		cancel() // 取消主上下文
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
	} `mapstructure:"cluster"`

	Auth struct {
		Enabled        bool   `mapstructure:"enabled"`
		SecretKey      string `mapstructure:"secret_key"`
		Issuer         string `mapstructure:"issuer"`
		AllowAnonymous bool   `mapstructure:"allow_anonymous"`
	} `mapstructure:"auth"`

	// 添加版本字段
	Version string `mapstructure:"version"`
}

// 加载配置
func loadConfig(configFile string) (Config, error) {
	viper.SetConfigFile(configFile)

	// 设置环境变量前缀和分隔符
	viper.SetEnvPrefix("GOHUB")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	var config Config

	if err := viper.ReadInConfig(); err != nil {
		return config, err
	}

	if err := viper.Unmarshal(&config); err != nil {
		return config, err
	}

	// 设置默认值
	if config.Auth.SecretKey == "" {
		config.Auth.SecretKey = "default-secret-key-change-this-in-production"
	}

	if config.Auth.Issuer == "" {
		config.Auth.Issuer = "gohub"
	}

	return config, nil
}

// 生成客户端ID
func generateClientID() string {
	return uuid.New().String()
}
