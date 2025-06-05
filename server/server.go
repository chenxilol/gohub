package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/chenxilol/gohub/configs"
	"github.com/chenxilol/gohub/internal/dispatcher"
	"github.com/chenxilol/gohub/internal/handlers"
	"github.com/chenxilol/gohub/internal/metrics"
	internalwebsocket "github.com/chenxilol/gohub/internal/websocket"
	"github.com/chenxilol/gohub/pkg/auth"
	"github.com/chenxilol/gohub/pkg/bus"
	hubnats "github.com/chenxilol/gohub/pkg/bus/nats"
	"github.com/chenxilol/gohub/pkg/bus/noop"
	hubredis "github.com/chenxilol/gohub/pkg/bus/redis"
	"github.com/chenxilol/gohub/pkg/hub"
	"github.com/chenxilol/gohub/pkg/sdk"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HandlerFunc func(ctx context.Context, client *hub.Client, data json.RawMessage) error

// EventHandlerFunc 是事件处理函数的类型
type EventHandlerFunc func(ctx context.Context, event sdk.Event) error

// Options 服务器配置选项
type Options struct {
	// HTTP服务器地址，默认 ":8080"
	Address string

	// 是否启用集群模式，默认 false
	EnableCluster bool

	// 消息总线类型: "nats", "redis", "noop"，默认 "noop"
	BusType string

	// NATS配置，当BusType为"nats"时使用
	NATSConfig *hubnats.Config

	// Redis配置，当BusType为"redis"时使用
	RedisConfig *hubredis.Config

	// JWT认证配置
	EnableAuth     bool   // 是否启用认证，默认 false
	AllowAnonymous bool   // 是否允许匿名连接，默认 true
	JWTSecretKey   string // JWT密钥
	JWTIssuer      string // JWT签发者

	// WebSocket配置
	ReadTimeout      time.Duration // 读超时，默认 60s
	WriteTimeout     time.Duration // 写超时，默认 60s
	ReadBufferSize   int           // 读缓冲区大小，默认 4KB
	WriteBufferSize  int           // 写缓冲区大小，默认 4KB
	MessageBufferCap int           // 消息缓冲区容量，默认 256

	// 日志级别: "debug", "info", "warn", "error"，默认 "info"
	LogLevel string

	// 自定义WebSocket升级器
	Upgrader *websocket.Upgrader
}

// Server GoHub服务器
type Server struct {
	options     *Options
	config      *configs.Config
	authService *auth.JWTService
	messageBus  bus.MessageBus
	hub         *hub.Hub
	sdk         *sdk.GoHubSDK
	dispatcher  hub.MessageDispatcher
	httpServer  *http.Server
	upgrader    *websocket.Upgrader
	customMux   *http.ServeMux // 允许用户添加自定义路由
}

// NewServer 创建一个新的GoHub服务器
func NewServer(opts *Options) (*Server, error) {
	if opts == nil {
		opts = defaultOptions()
	} else {
		fillDefaults(opts)
	}
	config := buildConfig(opts)
	setupLogging(opts.LogLevel)

	s := &Server{
		options:   opts,
		config:    config,
		customMux: http.NewServeMux(),
	}

	if opts.Upgrader != nil {
		s.upgrader = opts.Upgrader
	} else {
		s.upgrader = &websocket.Upgrader{
			ReadBufferSize:  opts.ReadBufferSize,
			WriteBufferSize: opts.WriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}
	}

	if err := s.initComponents(); err != nil {
		return nil, err
	}
	handlers.RegisterHandlers(s.dispatcher)
	s.setupRoutes()

	return s, nil
}

// RegisterHandler 注册自定义消息处理器
func (s *Server) RegisterHandler(messageType string, handler HandlerFunc) error {
	return s.sdk.RegisterMessageHandler(messageType, hub.MessageHandlerFunc(handler))
}

// On 注册事件处理器
func (s *Server) On(eventType sdk.EventType, handler EventHandlerFunc) {
	s.sdk.On(eventType, func(ctx context.Context, event sdk.Event) error {
		return handler(ctx, event)
	})
}

// Handle 添加自定义HTTP路由
func (s *Server) Handle(pattern string, handler http.Handler) {
	s.customMux.Handle(pattern, handler)
}

// HandleFunc 添加自定义HTTP处理函数
func (s *Server) HandleFunc(pattern string, handler http.HandlerFunc) {
	s.customMux.HandleFunc(pattern, handler)
}

// SDK 获取GoHub SDK实例
func (s *Server) SDK() *sdk.GoHubSDK {
	return s.sdk
}

// Start 启动服务器
func (s *Server) Start() error {
	slog.Info("Starting GoHub server", "address", s.options.Address)

	// 初始化Prometheus指标
	metrics.Default()

	// 创建主路由
	mainMux := http.NewServeMux()

	// 注册核心路由
	mainMux.HandleFunc("/ws", s.handleWebSocket)
	mainMux.Handle("/metrics", promhttp.HandlerFor(metrics.GetRegistry(), promhttp.HandlerOpts{}))
	mainMux.HandleFunc("/health", s.handleHealth)

	// 合并自定义路由
	mainMux.Handle("/", s.customMux)

	s.httpServer = &http.Server{
		Addr:    s.options.Address,
		Handler: mainMux,
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown 优雅关闭服务器
func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down GoHub server...")

	// 关闭SDK
	if err := s.sdk.Close(); err != nil {
		slog.Error("Failed to close SDK", "error", err)
	}

	// 关闭Hub
	if err := s.hub.Close(); err != nil {
		slog.Error("Failed to close hub", "error", err)
	}

	// 关闭消息总线
	if s.messageBus != nil {
		if err := s.messageBus.Close(); err != nil {
			slog.Error("Failed to close message bus", "error", err)
		}
	}

	// 关闭HTTP服务器
	return s.httpServer.Shutdown(ctx)
}

// initComponents 初始化内部组件
func (s *Server) initComponents() error {
	var err error
	if s.options.EnableCluster {
		s.messageBus, err = createMessageBus(s.config.Cluster)
		if err != nil {
			return fmt.Errorf("failed to create message bus: %w", err)
		}
	} else {
		s.messageBus = noop.New()
	}

	// 创建认证服务
	if s.options.EnableAuth {
		s.authService = auth.NewJWTService(s.options.JWTSecretKey, s.options.JWTIssuer)
	}

	// 创建分发器
	s.dispatcher = dispatcher.GetDispatcher()

	// 创建Hub
	s.hub = hub.NewHub(s.messageBus, s.config.Hub)

	// 创建SDK
	s.sdk = sdk.NewSDK(s.hub, s.dispatcher)

	return nil
}

// setupRoutes 设置HTTP路由
func (s *Server) setupRoutes() {
	// 健康检查已在Start中注册
}

// handleWebSocket 处理WebSocket连接
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// 认证
	var claims *auth.TokenClaims
	if s.options.EnableAuth {
		token := extractToken(r)
		if token != "" {
			var err error
			claims, err = s.authService.Authenticate(r.Context(), token)
			if err != nil {
				slog.Warn("WebSocket authentication failed", "error", err, "remoteAddr", r.RemoteAddr)
				http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
				metrics.RecordAuthFailure()
				return
			}
			metrics.RecordAuthSuccess()
		} else if !s.options.AllowAnonymous {
			slog.Warn("WebSocket connection attempt without token", "remoteAddr", r.RemoteAddr)
			http.Error(w, "Unauthorized: Token required", http.StatusUnauthorized)
			metrics.RecordAuthFailure()
			return
		}
	}

	// 升级连接
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("Failed to upgrade WebSocket", "error", err, "remoteAddr", r.RemoteAddr)
		metrics.RecordError()
		return
	}

	// 生成客户端ID
	clientID := generateClientID(r, claims)

	// 创建客户端
	wsAdapter := internalwebsocket.NewGorillaConn(conn)
	ctx := context.WithValue(context.Background(), "hub", s.hub)

	onClose := func(id string) {
		s.hub.Unregister(id)
		metrics.ClientDisconnected()
		s.sdk.TriggerEvent(ctx, sdk.Event{
			Type:     sdk.EventClientDisconnected,
			ClientID: id,
			Time:     time.Now(),
			Claims:   claims,
		})
		slog.Info("Client disconnected", "clientID", id)
	}

	client := hub.NewClient(ctx, clientID, wsAdapter, s.config.Hub, onClose, s.dispatcher)

	if claims != nil {
		client.SetAuthClaims(claims)
	}

	// 注册客户端
	s.hub.Register(client)
	metrics.ClientConnected()

	// 触发连接事件
	s.sdk.TriggerEvent(ctx, sdk.Event{
		Type:     sdk.EventClientConnected,
		ClientID: clientID,
		Time:     time.Now(),
		Claims:   claims,
	})

	slog.Info("Client connected",
		"clientID", clientID,
		"authenticated", claims != nil,
		"username", getUsername(claims),
		"remoteAddr", r.RemoteAddr)
}

// handleHealth 处理健康检查
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"status":  "ok",
		"version": s.config.Version,
		"time":    time.Now().Format(time.RFC3339),
	}
	_ = json.NewEncoder(w).Encode(response)
}

// 辅助函数

func defaultOptions() *Options {
	return &Options{
		Address:          ":8080",
		EnableCluster:    false,
		BusType:          "noop",
		AllowAnonymous:   true,
		ReadTimeout:      60 * time.Second,
		WriteTimeout:     60 * time.Second,
		ReadBufferSize:   4 * 1024,
		WriteBufferSize:  4 * 1024,
		MessageBufferCap: 256,
		LogLevel:         "info",
	}
}

func fillDefaults(opts *Options) {
	if opts.Address == "" {
		opts.Address = ":8080"
	}
	if opts.BusType == "" {
		opts.BusType = "noop"
	}
	if opts.ReadTimeout == 0 {
		opts.ReadTimeout = 60 * time.Second
	}
	if opts.WriteTimeout == 0 {
		opts.WriteTimeout = 60 * time.Second
	}
	if opts.ReadBufferSize == 0 {
		opts.ReadBufferSize = 4 * 1024
	}
	if opts.WriteBufferSize == 0 {
		opts.WriteBufferSize = 4 * 1024
	}
	if opts.MessageBufferCap == 0 {
		opts.MessageBufferCap = 256
	}
	if opts.LogLevel == "" {
		opts.LogLevel = "info"
	}
	if opts.JWTIssuer == "" {
		opts.JWTIssuer = "gohub"
	}
}

func buildConfig(opts *Options) *configs.Config {
	config := configs.NewDefaultConfig()

	config.Server.Addr = opts.Address
	config.Server.Hub.ReadTimeout = opts.ReadTimeout
	config.Server.Hub.WriteTimeout = opts.WriteTimeout
	config.Server.Hub.ReadBufferSize = opts.ReadBufferSize
	config.Server.Hub.WriteBufferSize = opts.WriteBufferSize
	config.Server.Hub.MessageBufferCap = opts.MessageBufferCap

	config.Cluster.Enabled = opts.EnableCluster
	config.Cluster.BusType = opts.BusType

	if opts.NATSConfig != nil {
		config.Cluster.NATS = *opts.NATSConfig
	}
	if opts.RedisConfig != nil {
		config.Cluster.Redis = *opts.RedisConfig
	}

	config.Auth.Enabled = opts.EnableAuth
	config.Auth.AllowAnonymous = opts.AllowAnonymous
	config.Auth.SecretKey = opts.JWTSecretKey
	config.Auth.Issuer = opts.JWTIssuer

	config.Log.Level = opts.LogLevel
	config.Version = "1.0.0"

	return &config
}

func setupLogging(level string) {
	logLevel := configs.ParseLogLevel(level)
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel})
	slog.SetDefault(slog.New(handler))
}

func createMessageBus(cluster configs.Cluster) (bus.MessageBus, error) {
	switch cluster.BusType {
	case "nats":
		return hubnats.New(cluster.NATS)
	case "redis":
		return hubredis.New(cluster.Redis)
	case "noop":
		return noop.New(), nil
	default:
		return nil, fmt.Errorf("unsupported bus type: %s", cluster.BusType)
	}
}

func extractToken(r *http.Request) string {
	// 从查询参数获取
	token := r.URL.Query().Get("token")
	if token != "" {
		return token
	}

	// 从Authorization头获取
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}

	return ""
}

func generateClientID(r *http.Request, claims *auth.TokenClaims) string {
	// 优先使用claims中的UserID
	if claims != nil && claims.UserID != "" {
		return claims.UserID
	}

	// 从查询参数获取
	clientID := r.URL.Query().Get("client_id")
	if clientID != "" {
		return clientID
	}

	// 生成新ID
	return uuid.New().String()
}

func getUsername(claims *auth.TokenClaims) string {
	if claims != nil && claims.Username != "" {
		return claims.Username
	}
	return "anonymous"
}
