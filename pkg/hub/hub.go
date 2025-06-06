package hub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/chenxilol/gohub/internal/metrics"
	"github.com/chenxilol/gohub/internal/utils"
	"github.com/chenxilol/gohub/pkg/bus"

	"github.com/gorilla/websocket"
)

var (
	ErrClientNotFound = errors.New("client not found")
)

// Hub 管理WebSocket客户端连接和消息分发
type Hub struct {
	clients      sync.Map     // key=id, value=*Client
	roomManager  *RoomManager // 添加房间管理器
	bus          bus.MessageBus
	cfg          Config
	ctx          context.Context
	cancel       context.CancelFunc
	deduplicator *MessageDeduplicator // 消息去重器
	nodeID       string               // 节点ID，用于标识不同的Hub实例
}

const (
	BroadcastTopic = "broadcast" // 广播消息
	UnicastPrefix  = "unicast/"  // 单播前缀
)

func FormatUnicastTopic(clientID string) string {
	return UnicastPrefix + clientID
}

func (c *Config) WithBusTimeout(timeout time.Duration) *Config {
	c.BusTimeout = timeout
	return c
}

func NewHub(messageBus bus.MessageBus, cfg Config) *Hub {
	if messageBus == nil {
		slog.Error("message bus is nil, hub will not work correctly")
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	nodeID := generateNodeID()
	h := &Hub{
		bus:          messageBus,
		cfg:          cfg,
		ctx:          ctx,
		cancel:       cancel,
		nodeID:       nodeID,
		deduplicator: NewMessageDeduplicator(nodeID, 30*time.Second),
	}

	h.roomManager = NewRoomManager(ctx, messageBus, h.deduplicator)
	h.setupBusSubscriptions(ctx)

	slog.Info("Hub initialized", "node_id", nodeID)
	return h
}

func generateNodeID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%d-%x", hostname, os.Getpid(), time.Now().UnixNano())
}

func (h *Hub) setupBusSubscriptions(ctx context.Context) {
	go func() {
		var broadcastCh <-chan []byte
		subscribeToBus := func() error {
			var err error
			broadcastCh, err = h.bus.Subscribe(ctx, BroadcastTopic)
			return err
		}

		for {
			select {
			case <-ctx.Done():
				slog.Info("context done, stopping broadcast subscription routine")
				return
			default:
			}

			err := utils.RetryWithBackoff(ctx, "bus_broadcast_subscribe", 5, time.Second, time.Minute, subscribeToBus)

			if err != nil {
				slog.Error("failed to subscribe to broadcast topic after multiple retries, broadcast won't be received from bus", "error", err)
				metrics.RecordCriticalError("failed_to_subscribe_broadcast")
				return
			}

			if processingErr := h.processBroadcastMessages(ctx, broadcastCh); processingErr != nil {
				if errors.Is(processingErr, context.Canceled) || errors.Is(processingErr, context.DeadlineExceeded) {
					slog.Info("broadcast message processing stopped due to context completion", "reason", processingErr)
					return
				}
			}

			slog.Info("broadcast message processing finished, will attempt to resubscribe")
			time.Sleep(100 * time.Millisecond)
		}
	}()
}

// processBroadcastMessages 处理从给定的广播通道接收消息，直到上下文完成或通道关闭。
func (h *Hub) processBroadcastMessages(ctx context.Context, broadcastCh <-chan []byte) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-broadcastCh:
			if !ok {
				slog.Warn("broadcast channel from bus closed")
				return errors.New("broadcast channel closed")
			}
			h.processBusMessage(msg)
		}
	}
}

func (h *Hub) processBusMessage(data []byte) {
	// 1. 首先尝试作为bus.Message解析，提取Data字段
	var wrappedMsg bus.Message
	if json.Unmarshal(data, &wrappedMsg) == nil && len(wrappedMsg.Data) > 0 {
		data = wrappedMsg.Data
	}

	var busMsg BusMessage
	if json.Unmarshal(data, &busMsg) == nil && busMsg.ID.String() != "" {

		msgID := busMsg.ID.String()
		if h.deduplicator.IsDuplicate(msgID) {
			slog.Debug("ignoring duplicate bus message", "id", msgID)
			return
		}
		h.deduplicator.MarkProcessed(msgID)

		// 检查类型并广播
		if busMsg.Type == "broadcast" && len(busMsg.Payload) > 0 {
			var payload []byte = busMsg.Payload

			// 尝试解析JSON字符串
			var strMsg string
			if json.Unmarshal(busMsg.Payload, &strMsg) == nil {
				payload = []byte(strMsg)
			}

			h.localBroadcast(Frame{MsgType: websocket.TextMessage, Data: payload})
			slog.Debug("broadcast message processed", "id", msgID)
			return
		}

		if busMsg.Type != "broadcast" {
			slog.Warn("unknown bus message type", "type", busMsg.Type)
			return
		}
	}

	// 3. 如果前面的解析都失败，将消息作为纯文本广播
	slog.Debug("broadcasting raw message data")
	h.localBroadcast(Frame{MsgType: websocket.TextMessage, Data: data})
}

// Register 注册一个客户端到Hub
func (h *Hub) Register(c *Client) {
	oldClientExists := false
	if oldClient, loaded := h.clients.LoadOrStore(c.GetID(), c); loaded {
		// 如果ID已存在，关闭旧连接
		slog.Info("replacing existing connection", "client_id", c.GetID())
		oldC := oldClient.(*Client)
		oldC.shutdown()

		// 清理旧的单播订阅
		if h.bus != nil {
			unicastTopic := FormatUnicastTopic(c.GetID())
			_ = h.bus.Unsubscribe(unicastTopic)
			slog.Debug("cleaned up old unicast subscription", "client_id", c.GetID(), "topic", unicastTopic)
		}

		h.clients.Store(c.GetID(), c)
		oldClientExists = true
	}

	// 对于每个新客户端，订阅其单播消息(如果使用消息总线)
	if h.bus != nil {
		unicastTopic := FormatUnicastTopic(c.GetID())
		go h.subscribeClientUnicast(c.GetID(), unicastTopic)
		if oldClientExists {
			slog.Debug("re-established unicast subscription for replaced client", "client_id", c.GetID(), "topic", unicastTopic)
		}
	}

	slog.Info("client registered", "id", c.GetID(), "total", h.GetClientCount())
}

// processUnicastMessages 处理从给定的单播通道接收消息，直到上下文完成、通道关闭或客户端断开连接。
func (h *Hub) processUnicastMessages(ctx context.Context, clientID string, unicastCh <-chan []byte) error {
	slog.Debug("starting to process unicast messages", "client", clientID)
	defer slog.Debug("stopped processing unicast messages", "client", clientID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-unicastCh:
			if !ok {
				slog.Warn("unicast channel closed", "client", clientID)
				return errors.New("unicast channel closed")
			}

			// 检查客户端是否仍然存在
			client, exists := h.clients.Load(clientID)
			if !exists {
				slog.Info("client no longer connected during message processing", "client", clientID)
				return errors.New("client disconnected")
			}

			// 发送消息到客户端
			frame := Frame{
				MsgType: websocket.TextMessage,
				Data:    msg,
			}
			if err := client.(*Client).Send(frame); err != nil {
				slog.Debug("failed to deliver unicast message", "client", clientID, "error", err)
				// 发送失败不中断处理，继续处理下一条消息
			}
		}
	}
}

// subscribeClientUnicast 为客户端订阅单播消息
func (h *Hub) subscribeClientUnicast(clientID, topic string) {
	var unicastCh <-chan []byte
	subscribeToUnicast := func() error {
		var err error
		unicastCh, err = h.bus.Subscribe(h.ctx, topic)
		return err
	}

	for { // 无限循环，用于在通道关闭时尝试重新订阅
		select {
		case <-h.ctx.Done():
			slog.Info("context done, stopping unicast subscription routine", "client", clientID)
			return
		default:
		}

		if _, exists := h.clients.Load(clientID); !exists {
			slog.Info("client no longer connected, stopping unicast subscription", "client", clientID)
			return
		}

		slog.Debug("attempting to establish unicast subscription", "client", clientID, "topic", topic)
		err := utils.RetryWithBackoff(h.ctx, fmt.Sprintf("unicast_subscribe_%s", clientID), 5, 500*time.Millisecond, 10*time.Second, subscribeToUnicast)

		if err != nil {
			slog.Error("failed to subscribe to unicast topic after multiple retries", "topic", topic, "client", clientID, "error", err)
			return
		}

		slog.Debug("successfully subscribed to unicast topic", "topic", topic, "client", clientID)

		processingErr := h.processUnicastMessages(h.ctx, clientID, unicastCh)

		if processingErr != nil {
			if errors.Is(processingErr, context.Canceled) || errors.Is(processingErr, context.DeadlineExceeded) {
				slog.Info("unicast message processing stopped due to context completion", "client", clientID, "reason", processingErr)
				return
			}
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// Unregister 从Hub移除客户端
func (h *Hub) Unregister(id string) {
	if _, exists := h.clients.LoadAndDelete(id); exists {
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

	if h.bus != nil {
		topic := FormatUnicastTopic(id)
		ctx, cancel := context.WithTimeout(context.Background(), h.cfg.BusTimeout)
		defer cancel()

		if err := h.bus.Publish(ctx, topic, f.Data); err != nil {
			slog.Warn("failed to publish unicast via bus", "client", id, "error", err)
			return ErrClientNotFound
		}
		return nil
	}

	return ErrClientNotFound
}

// Broadcast 向所有连接的客户端广播消息
func (h *Hub) Broadcast(f Frame) {
	h.localBroadcast(f)

	if h.bus == nil {
		return
	}

	msgID := h.deduplicator.GenerateID()

	// 处理消息载荷
	jsonPayload, err := prepareJsonPayload(f)
	if err != nil {
		slog.Error("failed to prepare JSON payload", "error", err)
		return
	}

	// 构造带有元数据的消息
	busMsg := BusMessage{
		ID:      msgID,
		Type:    "broadcast",
		Payload: jsonPayload,
		SentAt:  time.Now(),
	}

	// 将消息标记为已处理，避免稍后被自己处理
	h.deduplicator.MarkProcessed(msgID.String())

	msgData, err := json.Marshal(busMsg)
	if err != nil {
		slog.Error("failed to marshal broadcast message", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.BusTimeout)
	defer cancel()

	if err := h.bus.Publish(ctx, BroadcastTopic, msgData); err != nil {
		slog.Warn("failed to publish broadcast via bus", "error", err)
	}
}

// prepareJsonPayload 准备消息的JSON载荷
func prepareJsonPayload(f Frame) (json.RawMessage, error) {
	if f.MsgType == websocket.TextMessage {
		if json.Valid(f.Data) {
			return f.Data, nil
		}
		jsonString, err := json.Marshal(string(f.Data))
		if err != nil {
			return nil, fmt.Errorf("marshal message content to JSON string: %w", err)
		}
		return jsonString, nil
	}

	// 对于二进制消息，将其编码为base64字符串
	b64Str := base64.StdEncoding.EncodeToString(f.Data)
	jsonString, err := json.Marshal(b64Str)
	if err != nil {
		return nil, fmt.Errorf("marshal binary message to base64 JSON: %w", err)
	}
	return jsonString, nil
}

// localBroadcast 执行本地广播，向所有本地客户端发送消息
func (h *Hub) localBroadcast(f Frame) {
	count := 0
	h.clients.Range(func(_, v interface{}) bool {
		client := v.(*Client)
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
	h.clients.Range(func(k, v interface{}) bool {
		client := v.(*Client)
		client.shutdown()
		return true
	})

	h.roomManager.Close()
	if h.bus != nil {
		return h.bus.Close()
	}

	return nil
}

func (h *Hub) GetClient(id string) (*Client, error) {
	if client, ok := h.clients.Load(id); ok {
		return client.(*Client), nil
	}
	return nil, ErrClientNotFound
}
