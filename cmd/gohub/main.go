package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
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

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var (
	configFile = flag.String("config", "configs/config.yaml", "应用程序配置文件路径")
	appPort    = flag.Int("port", 8084, "应用程序服务监听端口")
)

// globalUpgrader 用于将 HTTP 连接升级到 WebSocket
var globalUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// 在生产环境中，这里应该有更严格的来源检查
		return true
	},
}

// AppServer 封装了使用 GoHub 库的应用程序服务器
type AppServer struct {
	config        *configs.Config       // GoHub 的配置结构
	authService   *auth.JWTService      // JWT 认证服务
	gohubSDK      *sdk.GoHubSDK         // GoHub SDK 实例
	httpServer    *http.Server          // HTTP 服务器
	messageBus    bus.MessageBus        // 消息总线实例
	hubInstance   *hub.Hub              // GoHub Hub 核心实例
	appDispatcher hub.MessageDispatcher // GoHub 消息分发器接口
}

// NewAppServer 创建并初始化一个新的应用程序服务器实例
func NewAppServer(cfg *configs.Config) (*AppServer, error) {
	var err error
	app := &AppServer{
		config: cfg,
	}

	if cfg.Cluster.Enabled {
		slog.Info("集群模式已启用，正在创建消息总线", "bus_type", cfg.Cluster.BusType)
		app.messageBus, err = createMessageBus(cfg.Cluster)
		if err != nil {
			return nil, fmt.Errorf("创建消息总线失败: %w", err)
		}
	} else {
		slog.Info("集群模式已禁用，使用 NoOpBus")
		app.messageBus = noop.New()
	}

	app.authService = auth.NewJWTService(cfg.Auth.SecretKey, cfg.Auth.Issuer)
	app.appDispatcher = dispatcher.GetDispatcher()
	app.hubInstance = hub.NewHub(app.messageBus, cfg.Hub)
	app.gohubSDK = sdk.NewSDK(app.hubInstance, app.appDispatcher)
	handlers.RegisterHandlers(app.appDispatcher)
	app.registerCustomMessageHandlers()
	app.setupCustomSDKEventHandlers()
	mux := http.NewServeMux()
	app.setupAppRoutes(mux)
	app.httpServer = &http.Server{
		Addr:    getServerAddr(cfg, *appPort),
		Handler: mux,
	}

	return app, nil
}

func (app *AppServer) registerCustomMessageHandlers() {
	slog.Info("正在注册应用程序自定义消息处理器...")

	// 示例：注册一个名为 "get_user_profile" 的处理器
	err := app.gohubSDK.RegisterMessageHandler("get_user_profile", handleGetUserProfile)
	if err != nil {
		slog.Error("注册 'get_user_profile' 处理器失败", "error", err)
		// 根据业务需求决定是否 panic 或记录错误后继续
	} else {
		slog.Info("已成功注册自定义消息处理器: get_user_profile")
	}

	// 示例：注册一个名为 "submit_order" 的处理器
	err = app.gohubSDK.RegisterMessageHandler("submit_order", handleSubmitOrder)
	if err != nil {
		slog.Error("注册 'submit_order' 处理器失败", "error", err)
	} else {
		slog.Info("已成功注册自定义消息处理器: submit_order")
	}
	// ... 在这里注册更多自定义处理器
}

// setupCustomSDKEventHandlers 设置应用程序自定义的 SDK 事件回调
func (app *AppServer) setupCustomSDKEventHandlers() {
	app.gohubSDK.On(sdk.EventClientConnected, func(ctx context.Context, event sdk.Event) error {
		username := "anonymous"
		if event.Claims != nil { // 检查 Claims 是否为 nil
			username = event.Claims.Username
		}
		slog.Info("应用程序逻辑：客户端已连接", "clientID", event.ClientID, "username", username)
		// 例如：可以加载用户数据，更新在线状态等
		return nil
	})

	app.gohubSDK.On(sdk.EventClientDisconnected, func(ctx context.Context, event sdk.Event) error {
		username := "anonymous"
		if event.Claims != nil { // <<<<< 添加这个检查
			username = event.Claims.Username
		}
		slog.Info("应用程序逻辑：客户端已断开", "clientID", event.ClientID, "username", username)
		// 例如：清理用户会话，更新离线状态等
		return nil
	})

	// 可以订阅更多事件，如 sdk.EventRoomCreated, sdk.EventClientMessage 等
}

// setupAppRoutes 设置此应用程序的 HTTP 路由
func (app *AppServer) setupAppRoutes(mux *http.ServeMux) {
	// GoHub WebSocket 端点
	mux.HandleFunc("/ws", app.handleAppWebSocket) // 应用自己的 WebSocket 处理函数

	// GoHub Prometheus 指标端点
	mux.Handle("/metrics", promhttp.HandlerFor(
		metrics.GetRegistry(), // 使用 GoHub 的 Prometheus 注册表
		promhttp.HandlerOpts{},
	))

	// GoHub 健康检查端点 (如果希望直接暴露)
	// 或者应用可以有自己的健康检查，内部调用 GoHub 的某些状态检查
	mux.HandleFunc("/gohub/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// 此处可以添加更多应用层面的健康检查逻辑
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "gohub_version": app.config.Version})
	})

	// 应用程序自定义的 HTTP API 端点
	mux.HandleFunc("/api/app/info", func(w http.ResponseWriter, r *http.Request) {
		// 示例：使用 GoHub SDK 获取信息
		clientCount := app.gohubSDK.GetClientCount()
		roomCount := app.gohubSDK.GetRoomCount()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"appName":          "My Awesome App using GoHub",
			"gohubClientCount": clientCount,
			"gohubRoomCount":   roomCount,
		})
	})

	mux.HandleFunc("/api/broadcast", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		var requestBody struct {
			Message string `json:"message"`
		}

		bodyBytes, err := ioutil.ReadAll(r.Body) // 或者 io.ReadAll(r.Body) for Go 1.16+
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if err := json.Unmarshal(bodyBytes, &requestBody); err != nil {
			http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		if requestBody.Message == "" {
			http.Error(w, "Message field cannot be empty", http.StatusBadRequest)
			return
		}

		// 使用 SDK 进行广播
		err = app.gohubSDK.BroadcastAll([]byte(requestBody.Message))
		if err != nil {
			slog.Error("Failed to broadcast message via API", "error", err)
			http.Error(w, "Failed to broadcast message", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Message broadcasted successfully"))
	})

}

// handleAppWebSocket 是应用程序处理 WebSocket 连接的函数
// 它利用 GoHub 的核心功能来管理连接和消息
func (app *AppServer) handleAppWebSocket(w http.ResponseWriter, r *http.Request) {
	// 1. 认证 (与 GoHub cmd/main.go 中的逻辑类似)
	token := r.URL.Query().Get("token")
	if token == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimSpace(authHeader[7:])
		}
	}

	var claims *auth.TokenClaims
	var authErr error
	if token != "" {
		claims, authErr = app.authService.Authenticate(r.Context(), token)
		if authErr != nil {
			slog.Warn("WebSocket 认证失败", "error", authErr, "remoteAddr", r.RemoteAddr)
			http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
			metrics.RecordAuthFailure() // 使用 GoHub 的 metrics
			return
		}
		metrics.RecordAuthSuccess()
	} else if !app.config.Auth.AllowAnonymous { // 使用 GoHub 的配置
		slog.Warn("WebSocket 连接尝试，但未提供令牌且不允许匿名访问", "remoteAddr", r.RemoteAddr)
		http.Error(w, "Unauthorized: Token required", http.StatusUnauthorized)
		metrics.RecordAuthFailure()
		return
	}

	// 2. 升级连接
	wsGorillaConn, err := globalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("WebSocket 连接升级失败", "error", err, "remoteAddr", r.RemoteAddr)
		metrics.RecordError()
		return
	}

	// 3. 生成客户端 ID
	var clientID string
	if claims != nil && claims.UserID != "" {
		clientID = claims.UserID
	} else {
		// 如果是匿名用户或 token 中没有 UserID，可以从查询参数获取或生成唯一 ID
		clientID = r.URL.Query().Get("client_id")
		if clientID == "" {
			clientID = uuid.New().String()
		}
	}

	// 4. 创建 GoHub Client
	// 使用 internalwebsocket.NewGorillaConn 包装原始连接
	wsConnAdapter := internalwebsocket.NewGorillaConn(wsGorillaConn) //

	// 创建客户端上下文，可以将 Hub 实例或其他应用级信息放入
	clientCtx := context.WithValue(context.Background(), "app_server_instance", app)
	clientCtx = context.WithValue(clientCtx, "hub", app.hubInstance) // 确保 handlers 能获取到 Hub

	// 定义 onClose 回调
	onClientClose := func(id string) {
		app.hubInstance.Unregister(id)
		metrics.ClientDisconnected()
		// 触发 SDK 事件
		app.gohubSDK.TriggerEvent(clientCtx, sdk.Event{
			Type:     sdk.EventClientDisconnected,
			ClientID: id,
			Time:     time.Now(),
			Claims:   claims, // 可以传递断开时的认证信息
		})
		slog.Info("GoHub 客户端已从 Hub 注销", "clientID", id)
	}

	// 使用 app.appDispatcher (类型为 hub.MessageDispatcher)
	gohubClient := hub.NewClient(
		clientCtx,
		clientID,
		wsConnAdapter,
		app.config.Hub, // 使用 GoHub 的 Hub 配置
		onClientClose,
		app.appDispatcher, // 传递应用持有的 dispatcher 实例
	)

	if claims != nil {
		gohubClient.SetAuthClaims(claims)
	}

	// 5. 注册客户端到 GoHub Hub
	app.hubInstance.Register(gohubClient)
	metrics.ClientConnected()

	// 触发 SDK 连接事件
	app.gohubSDK.TriggerEvent(clientCtx, sdk.Event{
		Type:     sdk.EventClientConnected,
		ClientID: clientID,
		Time:     time.Now(),
		Claims:   claims,
	})

	slog.Info("新的 WebSocket 客户端已连接并通过 GoHub 进行管理",
		"clientID", clientID,
		"authenticated", claims != nil,
		"username", func() string {
			if claims != nil {
				return claims.Username
			}
			return "anonymous"
		}())
}

// Start 启动应用程序服务器
func (app *AppServer) Start() error {
	slog.Info("正在启动应用程序服务器...", "address", app.httpServer.Addr)
	return app.httpServer.ListenAndServe()
}

// Shutdown 优雅地关闭应用程序服务器
func (app *AppServer) Shutdown(ctx context.Context) error {
	slog.Info("正在关闭应用程序服务器...")

	// 1. 关闭 GoHub SDK (如果它需要特殊关闭逻辑)
	if err := app.gohubSDK.Close(); err != nil {
		slog.Error("关闭 GoHub SDK 失败", "error", err)
		// 继续关闭其他组件
	}

	// 2. 关闭 GoHub Hub 核心
	// 这会处理所有客户端的断开和资源清理
	if err := app.hubInstance.Close(); err != nil {
		slog.Error("关闭 GoHub Hub 失败", "error", err)
	}

	// 3. 关闭消息总线 (如果已初始化)
	if app.messageBus != nil {
		if err := app.messageBus.Close(); err != nil {
			slog.Error("关闭消息总线失败", "error", err)
		}
	}

	// 4. 关闭 HTTP 服务器
	return app.httpServer.Shutdown(ctx)
}

// --- 自定义消息处理器示例 ---

// handleGetUserProfile 是一个自定义的 WebSocket 消息处理器
func handleGetUserProfile(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	slog.Info("处理自定义消息: get_user_profile", "clientID", client.ID(), "payload", string(data))

	// 示例：假设请求中包含 userID
	var request struct {
		TargetUserID string `json:"target_user_id"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		slog.Error("解析 get_user_profile 数据失败", "error", err)
		// 可以向客户端发送错误消息
		errMsg, _ := json.Marshal(map[string]interface{}{"message_type": "error", "data": map[string]string{"error": "invalid_payload"}})
		return client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: errMsg})
	}

	// 模拟获取用户数据
	profileData := map[string]interface{}{
		"userID":    request.TargetUserID,
		"nickname":  "User_" + request.TargetUserID,
		"email":     request.TargetUserID + "@example.com",
		"lastLogin": time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}

	// 构造完整的 WebSocket 消息进行回复
	responseMsg := map[string]interface{}{
		"message_type": "user_profile_data", // 自定义回复消息类型
		"data":         profileData,
	}
	responseBytes, err := json.Marshal(responseMsg)
	if err != nil {
		slog.Error("序列化 user_profile_data 失败", "error", err)
		return err
	}

	slog.Info("已获取用户 Profile，准备发送", "targetUserID", request.TargetUserID)
	return client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: responseBytes})
}

// handleSubmitOrder 是另一个自定义的 WebSocket 消息处理器
func handleSubmitOrder(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	slog.Info("处理自定义消息: submit_order", "clientID", client.ID(), "payload", string(data))

	// 检查用户是否有下单权限 (示例)
	if !client.HasPermission(auth.Permission("submit:order")) { // 假设有这样的权限定义
		slog.Warn("用户无权提交订单", "clientID", client.ID())
		errMsg, _ := json.Marshal(map[string]interface{}{"message_type": "error", "data": map[string]string{"error": "permission_denied", "message": "You do not have permission to submit orders."}})
		return client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: errMsg})
	}

	var orderRequest struct {
		ProductID string `json:"product_id"`
		Quantity  int    `json:"quantity"`
	}
	if err := json.Unmarshal(data, &orderRequest); err != nil {
		slog.Error("解析 submit_order 数据失败", "error", err)
		errMsg, _ := json.Marshal(map[string]interface{}{"message_type": "error", "data": map[string]string{"error": "invalid_order_payload"}})
		return client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: errMsg})
	}

	// 模拟订单处理...
	orderID := "ORD-" + uuid.New().String()[:8]
	slog.Info("订单处理中...", "productID", orderRequest.ProductID, "quantity", orderRequest.Quantity, "assignedOrderID", orderID)
	time.Sleep(50 * time.Millisecond) // 模拟处理延迟

	// 回复订单提交结果
	orderConfirmation := map[string]interface{}{
		"message_type": "order_confirmation",
		"data": map[string]interface{}{
			"orderID":   orderID,
			"productID": orderRequest.ProductID,
			"quantity":  orderRequest.Quantity,
			"status":    "Order Submitted Successfully",
		},
	}
	responseBytes, _ := json.Marshal(orderConfirmation)
	return client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: responseBytes})
}

// --- main 函数 ---
func main() {
	flag.Parse()

	// 1. 加载 GoHub 配置 (您的应用可能需要自己的配置文件，或者复用 GoHub 的)
	// 这里我们假设您的应用直接使用或扩展 GoHub 的配置结构
	gohubConfig, err := configs.LoadConfig(*configFile)
	if err != nil {
		// 如果配置文件不是 GoHub 原生的，您可能需要一个转换或映射步骤
		// 或者直接手动构建 configs.Config 结构
		slog.Error("加载 GoHub 配置文件失败，将使用默认配置", "error", err, "configFile", *configFile)
		defaultCfg := configs.NewDefaultConfig() // 使用 GoHub 的默认配置
		// 您可能需要根据应用需求调整这里的默认配置，特别是集群和总线类型
		defaultCfg.Cluster.Enabled = false                    // 例如，对于独立应用，默认禁用集群
		defaultCfg.Server.Addr = fmt.Sprintf(":%d", *appPort) // 使用命令行参数的端口
		gohubConfig = defaultCfg
	} else {
		// 如果配置文件加载成功，但命令行指定了端口，则覆盖
		if *appPort != 0 && getServerAddr(&gohubConfig, 0) != fmt.Sprintf(":%d", *appPort) {
			// 更新gohubConfig中的端口信息
			hostParts := strings.Split(gohubConfig.Server.Addr, ":")
			host := ""
			if len(hostParts) > 0 {
				host = hostParts[0]
			}
			gohubConfig.Server.Addr = fmt.Sprintf("%s:%d", host, *appPort)
		}
	}

	// 2. 设置日志级别 (使用 GoHub 配置中的日志级别)
	logLevel := configs.ParseLogLevel(gohubConfig.Log.Level)
	logHandlerOptions := &slog.HandlerOptions{Level: logLevel}
	var logHandler slog.Handler
	logHandler = slog.NewJSONHandler(os.Stdout, logHandlerOptions)
	logger := slog.New(logHandler)
	slog.SetDefault(logger)
	slog.Info("应用程序日志记录器已初始化", "level", logLevel.String())

	// 3. 初始化 Prometheus 指标 (复用 GoHub 的)
	metrics.Default() //

	// 4. 创建并运行 AppServer
	app, err := NewAppServer(&gohubConfig)
	if err != nil {
		slog.Error("创建 AppServer 失败", "error", err)
		os.Exit(1)
	}

	// 5. 启动服务器
	go func() {
		if err := app.Start(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP 服务器启动失败", "error", err)
			os.Exit(1)
		}
	}()

	// 6. 优雅地关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("正在关闭服务器...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := app.Shutdown(ctx); err != nil {
		slog.Error("服务器关闭失败", "error", err)
		os.Exit(1)
	}

	slog.Info("服务器已成功关闭")
}

// --- 辅助函数 (可以从 GoHub 的 cmd/main.go 中借鉴或根据需要调整) ---

// createMessageBus 根据配置创建并返回一个 MessageBus 实例
func createMessageBus(clusterConfig configs.Cluster) (bus.MessageBus, error) {
	switch clusterConfig.BusType {
	case "nats":
		return hubnats.New(clusterConfig.NATS)
	case "redis":
		return hubredis.New(clusterConfig.Redis)
	default:
		return nil, fmt.Errorf("不支持的消息总线类型: %s", clusterConfig.BusType)
	}
}

// getServerAddr 根据配置和命令行参数获取服务监听地址
func getServerAddr(config *configs.Config, cliPort int) string {
	addr := config.Server.Addr
	if cliPort != 0 {
		host := strings.Split(addr, ":")[0]
		// 如果配置文件中的地址只有端口（例如 ":8080"），host 会是空字符串
		// 此时我们希望监听所有接口，通常表示为 "0.0.0.0" 或 "" (net.Listen 会处理)
		// 为保持与原main.go一致性，如果host为空，使用空字符串（代表监听所有可用IPv4和IPv6接口）
		addr = fmt.Sprintf("%s:%d", host, cliPort)
	}
	return addr
}
