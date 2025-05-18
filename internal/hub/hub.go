package hub

import (
	"context"
	"encoding/json"
	"errors"
	"gohub/internal/bus"
	"log/slog"
	"sync"
	"time"
)

var (
	ErrClientNotFound = errors.New("client not found")
)

// Hub 管理WebSocket客户端连接和消息分发
type Hub struct {
	clients     sync.Map     // key=id, value=*Client
	roomManager *RoomManager // 添加房间管理器
	bus         bus.MessageBus
	cfg         Config
	ctx         context.Context
	cancel      context.CancelFunc
}

// 主题常量
const (
	BroadcastTopic = "broadcast" // 广播消息
	UnicastPrefix  = "unicast/"  // 单播前缀
)

// FormatUnicastTopic 格式化单播主题
func FormatUnicastTopic(clientID string) string {
	return UnicastPrefix + clientID
}

// WithBusTimeout Config 中添加总线超时配置
func (c *Config) WithBusTimeout(timeout time.Duration) *Config {
	c.BusTimeout = timeout
	return c
}

func NewHub(messageBus bus.MessageBus, cfg Config) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{
		bus:    messageBus,
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
	}

	// 初始化房间管理器
	h.roomManager = NewRoomManager(ctx, messageBus)

	if messageBus != nil {
		h.setupBusSubscriptions(ctx)
	}

	return h
}

// setupBusSubscriptions 设置消息总线订阅
func (h *Hub) setupBusSubscriptions(ctx context.Context) {
	// 订阅广播主题
	go func() {
		broadcastCh, err := h.bus.Subscribe(ctx, BroadcastTopic)
		if err != nil {
			slog.Error("failed to subscribe to broadcast topic", "error", err)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-broadcastCh:
				if !ok {
					return
				}
				// 处理从总线接收的广播消息
				h.processBusMessage(msg)
			}
		}
	}()
}

// 封装消息ID和时间戳，用于去重
type messageRecord struct {
	content   string    // 消息内容的哈希或本身
	timestamp time.Time // 消息接收时间
}

// processBusMessage 处理从消息总线接收的消息
func (h *Hub) processBusMessage(data []byte) {
	frame := Frame{
		MsgType: 1, // 文本消息
		Data:    data,
	}

	msgContent := string(data)
	slog.Debug("received message from bus", "content", msgContent)

	// 本地广播消息
	count := 0
	h.clients.Range(func(_, v interface{}) bool {
		client := v.(*Client)
		// 忽略发送错误，避免一个客户端影响所有人
		if err := client.Send(frame); err == nil {
			count++
		} else {
			slog.Debug("failed to deliver bus message to client",
				"client", client.ID(), "error", err)
		}
		return true
	})
	slog.Debug("bus message broadcast complete", "recipients", count)
}

// Register 注册一个客户端到Hub
func (h *Hub) Register(c *Client) {
	if oldClient, loaded := h.clients.LoadOrStore(c.ID(), c); loaded {
		// 如果ID已存在，关闭旧连接
		slog.Info("replacing existing connection", "client_id", c.ID())
		oldC := oldClient.(*Client)
		oldC.shutdown()
		h.clients.Store(c.ID(), c)
	}

	// 对于每个新客户端，订阅其单播消息(如果使用消息总线)
	if h.bus != nil {
		unicastTopic := FormatUnicastTopic(c.ID())
		go h.subscribeClientUnicast(c.ID(), unicastTopic)
	}

	slog.Info("client registered", "id", c.ID(), "total", h.GetClientCount())
}

// subscribeClientUnicast 为客户端订阅单播消息
func (h *Hub) subscribeClientUnicast(clientID, topic string) {
	unicastCh, err := h.bus.Subscribe(h.ctx, topic)
	if err != nil {
		slog.Error("failed to subscribe to unicast topic", "topic", topic, "error", err)
		return
	}

	for {
		select {
		case <-h.ctx.Done():
			return
		case msg, ok := <-unicastCh:
			if !ok {
				return
			}

			// 尝试发送消息到客户端
			if client, exists := h.clients.Load(clientID); exists {
				frame := Frame{
					MsgType: 1, // 文本消息
					Data:    msg,
				}
				if err := client.(*Client).Send(frame); err != nil {
					slog.Debug("failed to deliver unicast message", "client", clientID, "error", err)
				}
			} else {
				// 客户端不存在，取消订阅
				_ = h.bus.Unsubscribe(topic)
				return
			}
		}
	}
}

// Unregister 从Hub移除客户端
func (h *Hub) Unregister(id string) {
	if _, exists := h.clients.LoadAndDelete(id); exists {
		// 如果正在使用消息总线，取消单播订阅
		if h.bus != nil {
			_ = h.bus.Unsubscribe(FormatUnicastTopic(id))
		}
		slog.Info("client unregistered", "id", id, "remaining", h.GetClientCount())
	}
}

// Push 发送消息到特定客户端
func (h *Hub) Push(id string, f Frame) error {
	if v, ok := h.clients.Load(id); ok {
		return v.(*Client).Send(f)
	}

	// 如果启用了消息总线且客户端不在本地，尝试通过总线发送
	if h.bus != nil {
		topic := FormatUnicastTopic(id)
		ctx, cancel := context.WithTimeout(context.Background(), h.cfg.BusTimeout)
		defer cancel()

		if err := h.bus.Publish(ctx, topic, f.Data); err != nil {
			slog.Warn("failed to publish unicast via bus", "client", id, "error", err)
			return ErrClientNotFound
		}
		// 消息已通过总线发送，返回nil
		return nil
	}

	return ErrClientNotFound
}

// Broadcast 向所有连接的客户端广播消息
func (h *Hub) Broadcast(f Frame) {
	// 避免消息重复处理
	msgContent := string(f.Data)

	// 如果启用了消息总线，通过总线发布
	if h.bus != nil {
		ctx, cancel := context.WithTimeout(context.Background(), h.cfg.BusTimeout)
		defer cancel()

		// 尝试通过总线发布
		if err := h.bus.Publish(ctx, BroadcastTopic, f.Data); err != nil {
			slog.Warn("failed to publish broadcast via bus", "error", err)
		}
	}

	// 执行本地广播
	h.localBroadcast(f)

	slog.Debug("broadcast complete", "message", msgContent)
}

// localBroadcast 执行本地广播，向所有本地客户端发送消息
func (h *Hub) localBroadcast(f Frame) {
	count := 0
	h.clients.Range(func(_, v interface{}) bool {
		client := v.(*Client)
		// 忽略发送错误，避免一个客户端影响所有人
		if err := client.Send(f); err == nil {
			count++
		}
		return true
	})
	slog.Debug("local broadcast complete", "recipients", count)
}

// GetClientCount 获取当前连接的客户端数量
func (h *Hub) GetClientCount() int {
	count := 0
	h.clients.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	return count
}

// 以下是房间相关操作的便捷方法

// CreateRoom 创建一个新房间
func (h *Hub) CreateRoom(id, name string, maxClients int) (*Room, error) {
	return h.roomManager.CreateRoom(id, name, maxClients)
}

// DeleteRoom 删除一个房间
func (h *Hub) DeleteRoom(id string) error {
	return h.roomManager.DeleteRoom(id)
}

// GetRoom 获取一个房间
func (h *Hub) GetRoom(id string) (*Room, error) {
	return h.roomManager.GetRoom(id)
}

// ListRooms 列出所有房间
func (h *Hub) ListRooms() []*Room {
	return h.roomManager.ListRooms()
}

// JoinRoom 将客户端加入房间
func (h *Hub) JoinRoom(roomID string, clientID string) error {
	// 获取客户端
	clientObj, ok := h.clients.Load(clientID)
	if !ok {
		return ErrClientNotFound
	}

	client := clientObj.(*Client)
	return h.roomManager.JoinRoom(roomID, client)
}

// LeaveRoom 客户端离开房间
func (h *Hub) LeaveRoom(roomID string, clientID string) error {
	return h.roomManager.LeaveRoom(roomID, clientID)
}

// BroadcastToRoom 向房间内所有客户端广播消息
func (h *Hub) BroadcastToRoom(roomID string, frame Frame, excludeClientID string) error {
	// 如果启用了消息总线，并且消息总线不为空
	if h.bus != nil {
		// 构造房间消息（这里可以扩展，添加更多元数据）
		roomMsg := map[string]interface{}{
			"room_id": roomID,
			"data":    frame.Data,
			"exclude": excludeClientID,
			"type":    "room_broadcast",
		}

		// 序列化消息
		msgData, err := json.Marshal(roomMsg)
		if err != nil {
			slog.Error("failed to marshal room message", "error", err)
			return err
		}

		// 通过总线发布房间消息
		ctx, cancel := context.WithTimeout(context.Background(), h.cfg.BusTimeout)
		defer cancel()

		topic := FormatRoomTopic(roomID)
		if err := h.bus.Publish(ctx, topic, msgData); err != nil {
			slog.Warn("failed to publish room message via bus", "error", err)
			// 继续本地广播，不要返回错误
		}
	}

	// 执行本地房间广播
	return h.roomManager.BroadcastToRoom(roomID, frame.Data, excludeClientID)
}

// Close 关闭Hub及其所有客户端连接
func (h *Hub) Close() error {
	h.cancel()

	// 关闭所有客户端连接
	h.clients.Range(func(k, v interface{}) bool {
		client := v.(*Client)
		// 这将触发OnClose回调，从而调用Hub.Unregister
		client.shutdown()
		return true
	})

	// 关闭房间管理器
	h.roomManager.Close()

	// 关闭消息总线
	if h.bus != nil {
		return h.bus.Close()
	}

	return nil
}

// GetClient 获取指定ID的客户端
func (h *Hub) GetClient(id string) (*Client, error) {
	if client, ok := h.clients.Load(id); ok {
		return client.(*Client), nil
	}
	return nil, ErrClientNotFound
}
