package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"gohub/configs"
	"gohub/internal/auth"
	"gohub/internal/bus"
	hubnats "gohub/internal/bus/nats"
	"gohub/internal/bus/noop"
	hubreds "gohub/internal/bus/redis"
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
	port       = flag.Int("port", 0, "服务监听端口，如果指定，将覆盖配置文件中的设置")
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// GoHubServer 封装服务器逻辑，使其更易于测试和集成
type GoHubServer struct {
	config      *configs.Config
	authService *auth.JWTService
	wsHub       *hub.Hub
	gohubSDK    *sdk.GoHubSDK
	dispatcher  *dispatcher.Dispatcher
	srv         *http.Server
}

// NewGoHubServer 创建新的服务器实例
func NewGoHubServer(config *configs.Config) (*GoHubServer, error) {
	// 初始化消息总线
	var messageBus bus.MessageBus
	var err error
	if config.Cluster.Enabled {
		slog.Info("Cluster mode enabled, using message bus", "bus_type", config.Cluster.BusType)
		messageBus, err = createMessageBus(config.Cluster)
		if err != nil {
			return nil, fmt.Errorf("failed to create message bus: %w", err)
		}
	}

	// 创建认证服务
	authService := auth.NewJWTService(config.Auth.SecretKey, config.Auth.Issuer)

	// 创建WebSocket Hub
	wsHub := hub.NewHub(messageBus, config.Hub)

	// 创建SDK
	gohubSDK := sdk.NewSDK(wsHub)

	// 创建分发器并注册处理器
	d := dispatcher.GetDispatcher()
	handlers.RegisterHandlers(d)

	server := &GoHubServer{
		config:      config,
		authService: authService,
		wsHub:       wsHub,
		gohubSDK:    gohubSDK,
		dispatcher:  d,
	}

	// 设置事件监听器
	server.setupEventHandlers()

	// 设置HTTP路由
	server.setupRoutes()

	return server, nil
}

// setupEventHandlers 设置SDK事件处理器
func (s *GoHubServer) setupEventHandlers() {
	// 客户端连接事件
	s.gohubSDK.On(sdk.EventClientConnected, func(ctx context.Context, event sdk.Event) error {
		slog.Info("Client connected via SDK",
			"client_id", event.ClientID,
			"authenticated", event.Claims != nil,
			"time", event.Time)
		return nil
	})

	// 客户端断开事件
	s.gohubSDK.On(sdk.EventClientDisconnected, func(ctx context.Context, event sdk.Event) error {
		slog.Info("Client disconnected via SDK",
			"client_id", event.ClientID,
			"time", event.Time)
		return nil
	})

	// 客户端消息事件
	s.gohubSDK.On(sdk.EventClientMessage, func(ctx context.Context, event sdk.Event) error {
		slog.Debug("Client message received via SDK",
			"client_id", event.ClientID,
			"message_length", len(event.Message))
		return nil
	})

	// 房间事件处理
	s.gohubSDK.On(sdk.EventRoomCreated, func(ctx context.Context, event sdk.Event) error {
		slog.Info("Room created via SDK", "room_id", event.RoomID)
		return nil
	})

	s.gohubSDK.On(sdk.EventRoomJoined, func(ctx context.Context, event sdk.Event) error {
		slog.Info("Client joined room via SDK",
			"client_id", event.ClientID,
			"room_id", event.RoomID)
		return nil
	})
}

// setupRoutes 设置HTTP路由
func (s *GoHubServer) setupRoutes() {
	mux := http.NewServeMux()

	// Prometheus指标接口
	mux.Handle("/metrics", promhttp.HandlerFor(
		metrics.GetRegistry(),
		promhttp.HandlerOpts{},
	))

	// WebSocket连接入口
	mux.HandleFunc("/ws", s.handleWebSocket)

	// API端点 - 使用SDK进行操作
	mux.HandleFunc("/api/broadcast", s.handleBroadcast)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/rooms", s.handleRooms)
	mux.HandleFunc("/api/clients", s.handleClients)

	// 健康检查端点
	mux.HandleFunc("/health", s.handleHealth)

	s.srv = &http.Server{
		Addr:    getServerAddr(s.config, *port),
		Handler: mux,
	}
}

// handleWebSocket 处理WebSocket连接
func (s *GoHubServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 获取并验证认证令牌
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("Authorization")
		if strings.HasPrefix(token, "Bearer ") {
			token = token[7:]
		}
	}

	// 验证令牌
	var claims *auth.TokenClaims
	var authErr error
	if token != "" {
		claims, authErr = s.authService.Authenticate(r.Context(), token)
		if authErr != nil {
			slog.Error("Authentication failed", "error", authErr)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			metrics.RecordAuthFailure()
			return
		}
		metrics.RecordAuthSuccess()
	} else if !s.config.Auth.AllowAnonymous {
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
		clientID = claims.UserID
	} else {
		clientID = r.URL.Query().Get("client_id")
		if clientID == "" {
			clientID = generateClientID()
		}
	}

	// 创建上下文
	requestCtxWithHub := context.WithValue(context.Background(), "hub", s.wsHub)

	// 创建WebSocket客户端
	wsConn := internalwebsocket.NewGorillaConn(conn)
	client := hub.NewClient(requestCtxWithHub, clientID, wsConn, s.config.Hub, func(id string) {
		s.wsHub.Unregister(id)
		metrics.ClientDisconnected()

		// 触发SDK断开事件
		s.gohubSDK.TriggerEvent(requestCtxWithHub, sdk.Event{
			Type:     sdk.EventClientDisconnected,
			ClientID: id,
			Time:     time.Now(),
		})
	}, s.dispatcher)

	// 如果有认证声明，保存到客户端
	if claims != nil {
		client.SetAuthClaims(claims)
	}

	// 注册到Hub
	s.wsHub.Register(client)
	metrics.ClientConnected()

	// 触发SDK客户端连接事件
	s.gohubSDK.TriggerEvent(requestCtxWithHub, sdk.Event{
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
}

// handleBroadcast 处理广播API - 使用SDK
func (s *GoHubServer) handleBroadcast(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var msg struct {
		Message string `json:"message"`
	}

	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if msg.Message == "" {
		http.Error(w, "Message content cannot be empty", http.StatusBadRequest)
		return
	}

	// 使用SDK进行广播
	if err := s.gohubSDK.BroadcastAll([]byte(msg.Message)); err != nil {
		slog.Error("Broadcast failed", "error", err)
		http.Error(w, "Broadcast failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"broadcast initiated"}`))
	slog.Info("Broadcast API called", "message", msg.Message)
}

// handleStats 处理统计信息API - 使用SDK
func (s *GoHubServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := map[string]interface{}{
		"client_count": s.gohubSDK.GetClientCount(),
		"room_count":   s.gohubSDK.GetRoomCount(),
		"timestamp":    time.Now().Unix(),
		"cluster_mode": s.config.Cluster.Enabled,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleRooms 处理房间管理API - 使用SDK
func (s *GoHubServer) handleRooms(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// 列出所有房间
		rooms := s.gohubSDK.ListRooms()
		roomData := make([]map[string]interface{}, len(rooms))
		for i, room := range rooms {
			members, _ := s.gohubSDK.GetRoomMembers(room.ID)
			roomData[i] = map[string]interface{}{
				"id":           room.ID,
				"name":         room.Name,
				"member_count": len(members),
				"max_clients":  room.MaxClients,
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(roomData)

	case http.MethodPost:
		// 创建房间
		var req struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			MaxClients int    `json:"max_clients"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		if req.MaxClients <= 0 {
			req.MaxClients = 100 // 默认值
		}

		if err := s.gohubSDK.CreateRoom(req.ID, req.Name, req.MaxClients); err != nil {
			http.Error(w, "Failed to create room", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"status": "room created"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleClients 处理客户端管理API - 使用SDK
func (s *GoHubServer) handleClients(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取客户端统计信息
	clientCount := s.gohubSDK.GetClientCount()

	response := map[string]interface{}{
		"total_clients": clientCount,
		"timestamp":     time.Now().Unix(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleHealth 处理健康检查
func (s *GoHubServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok","version":"` + s.config.Version + `"}`))
}

// Start 启动服务器
func (s *GoHubServer) Start() error {
	slog.Info("Starting server", "addr", s.srv.Addr, "cluster_mode", s.config.Cluster.Enabled)
	return s.srv.ListenAndServe()
}

// Shutdown 优雅关闭服务器
func (s *GoHubServer) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down server...")

	// 关闭SDK
	if err := s.gohubSDK.Close(); err != nil {
		slog.Error("Failed to close SDK", "error", err)
	}

	// 关闭Hub
	s.wsHub.Close()

	// 关闭HTTP服务器
	return s.srv.Shutdown(ctx)
}

// GetSDK 返回SDK实例，方便外部使用和测试
func (s *GoHubServer) GetSDK() *sdk.GoHubSDK {
	return s.gohubSDK
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

	// 创建服务器实例
	server, err := NewGoHubServer(&config)
	if err != nil {
		slog.Error("Failed to create server", "error", err)
		os.Exit(1)
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

		// 给现有连接5秒时间关闭
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("Server shutdown error", "error", err)
		}
	}()

	// 启动服务器
	if err := server.Start(); !errors.Is(err, http.ErrServerClosed) {
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

// getServerAddr 根据配置和命令行参数获取服务监听地址
func getServerAddr(config *configs.Config, cliPort int) string {
	addr := config.Server.Addr
	if cliPort != 0 {
		host := strings.Split(addr, ":")[0]
		if host == "" {
			host = "0.0.0.0" // 默认为监听所有接口
		}
		addr = fmt.Sprintf("%s:%d", host, cliPort)
	}
	return addr
}

// createMessageBus 根据配置创建相应的消息总线
func createMessageBus(clusterConfig configs.Cluster) (bus.MessageBus, error) {
	switch strings.ToLower(clusterConfig.BusType) {
	case "nats":
		slog.Info("Creating NATS message bus")
		return hubnats.New(clusterConfig.NATS)
	case "redis":
		slog.Info("Creating Redis message bus")
		return hubreds.New(clusterConfig.Redis)
	case "noop", "":
		slog.Info("Creating NoOp message bus")
		return noop.New(), nil
	default:
		return nil, fmt.Errorf("unsupported message bus type: %s", clusterConfig.BusType)
	}
}
