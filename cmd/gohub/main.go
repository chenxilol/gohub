package main

import (
	"context"
	"errors"
	"flag"
	"gohub/internal/bus"
	"gohub/internal/dispatcher"
	"gohub/internal/handlers"
	hub2 "gohub/internal/hub"
	myws "gohub/internal/websocket"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

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
		return true // 允许所有跨域请求，生产环境应该限制
	},
}

func main() {
	// 解析命令行参数
	flag.Parse()

	// 初始化日志
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	// 加载配置
	config, err := loadConfig(*configFile)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 根据配置决定是否创建消息总线
	var messageBus hub2.MessageBus
	if config.Cluster.Enabled {
		slog.Info("Cluster mode enabled, connecting to etcd")
		etcdBus, err := bus.NewEtcdBus(bus.EtcdConfig{
			Endpoints:   config.Cluster.Etcd.Endpoints,
			DialTimeout: config.Cluster.Etcd.DialTimeout,
			KeyPrefix:   config.Cluster.Etcd.KeyPrefix,
		})
		if err != nil {
			slog.Error("Failed to create etcd message bus", "error", err)
			os.Exit(1)
		}
		messageBus = etcdBus
		defer etcdBus.Close()
	}

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

	// 注册消息处理函数
	d := dispatcher.GetDispatcher()
	handlers.RegisterHandlers(d)

	// HTTP处理函数：WebSocket连接入口
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			slog.Error("Failed to upgrade connection", "error", err)
			return
		}

		// 获取客户端ID（从URL参数、认证令牌或生成一个）
		clientID := r.URL.Query().Get("client_id")
		if clientID == "" {
			clientID = generateClientID()
		}

		// 创建WebSocket客户端
		wsConn := myws.NewGorillaConn(conn)
		client := hub2.NewClient(ctx, clientID, wsConn, hubConfig, func(id string) {
			wsHub.Unregister(id)
		}, d)

		// 注册到Hub
		wsHub.Register(client)

		slog.Info("New WebSocket connection established", "client_id", clientID)
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

	return config, nil
}

// 生成客户端ID
func generateClientID() string {
	return uuid.New().String()
}
