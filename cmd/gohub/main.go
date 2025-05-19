package main

import (
	"context"
	"errors"
	"flag"
	"gohub/configs"
	"gohub/internal/auth"
	"gohub/internal/bus"
	hubnats "gohub/internal/bus/nats"
	"gohub/internal/dispatcher"
	"gohub/internal/handlers"
	"gohub/internal/hub"
	"gohub/internal/metrics"
	"gohub/internal/sdk"
	internalwebsocket "gohub/internal/websocket"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var (
	configFile = flag.String("config", "configs/config.yaml", "配置文件路径")
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	flag.Parse()

	config, err := configs.LoadConfig(*configFile)
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	logLevel := configs.ParseLogLevel(config.Log.Level)
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	logger := slog.New(logHandler)
	slog.SetDefault(logger)
	slog.Info("Logger initialized", "level", config.Log.Level)

	metrics.Default()

	var messageBus bus.MessageBus
	if config.Cluster.Enabled {
		slog.Info("Cluster mode enabled, using message bus")
		messageBus, err = hubnats.New(config.Cluster.NATS)
		if err != nil {
			slog.Error("Failed to connect to NATS", "error", err)
			os.Exit(1)
		}
	}

	// 创建认证服务
	authService := auth.NewJWTService(config.Auth.SecretKey, config.Auth.Issuer)
	wsHub := hub.NewHub(messageBus, config.Hub)
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
		requestCtxWithHub := context.WithValue(context.Background(), "hub", wsHub)

		requestCtxWithHub.Value("hub")

		// 创建WebSocket客户端
		wsConn := internalwebsocket.NewGorillaConn(conn)
		client := hub.NewClient(requestCtxWithHub, clientID, wsConn, config.Hub, func(id string) {
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
		_, _ = w.Write([]byte(`{"status":"ok","version":"` + config.Version + `"}`))
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

// 生成客户端ID
func generateClientID() string {
	return uuid.New().String()
}
