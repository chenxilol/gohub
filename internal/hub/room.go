package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gohub/internal/bus"
	"gohub/internal/metrics"
	"log/slog"
	"sync"
	"time"
)

// 定义房间相关错误
var (
	ErrRoomNotFound        = errors.New("room not found")
	ErrRoomAlreadyExists   = errors.New("room already exists")
	ErrRoomIsFull          = errors.New("room is full")
	ErrClientNotInRoom     = errors.New("client not in room")
	ErrClientAlreadyInRoom = errors.New("client already in room")
)

// RoomTopicPrefix 格式化房间主题
const (
	RoomTopicPrefix = "room/"
)

// FormatRoomTopic 格式化房间主题名称
func FormatRoomTopic(roomID string) string {
	return RoomTopicPrefix + roomID
}

// Room 表示一个聊天房间或频道
type Room struct {
	ID         string             // 房间唯一标识
	Name       string             // 房间名称
	MaxClients int                // 最大客户端数量，0表示无限制
	clients    map[string]*Client // 房间内的客户端，key=clientID
	mu         sync.RWMutex       // 保护clients映射的互斥锁
}

// NewRoom 创建一个新房间
func NewRoom(id, name string, maxClients int) *Room {
	return &Room{
		ID:         id,
		Name:       name,
		MaxClients: maxClients,
		clients:    make(map[string]*Client),
	}
}

// AddClient 添加客户端到房间
func (r *Room) AddClient(client *Client) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查房间是否已满
	if r.MaxClients > 0 && len(r.clients) >= r.MaxClients {
		return ErrRoomIsFull
	}

	// 检查客户端是否已在房间内
	if _, exists := r.clients[client.ID()]; exists {
		return ErrClientAlreadyInRoom
	}

	// 添加客户端
	r.clients[client.ID()] = client

	// 记录指标
	metrics.RoomJoined()

	slog.Info("client joined room",
		"client_id", client.ID(),
		"room_id", r.ID,
		"room_name", r.Name,
		"total_clients", len(r.clients))

	return nil
}

// RemoveClient 从房间移除客户端
func (r *Room) RemoveClient(clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查客户端是否在房间内
	if _, exists := r.clients[clientID]; !exists {
		return ErrClientNotInRoom
	}

	// 移除客户端
	delete(r.clients, clientID)

	// 记录指标
	metrics.RoomLeft()

	slog.Info("client left room",
		"client_id", clientID,
		"room_id", r.ID,
		"room_name", r.Name,
		"remaining_clients", len(r.clients))

	return nil
}

// Broadcast 向房间内所有客户端广播消息
func (r *Room) Broadcast(message []byte, excludeClientID string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// 创建帧
	frame := Frame{
		MsgType: 1, // 文本消息
		Data:    message,
	}

	// 广播给所有房间内客户端
	count := 0
	for id, client := range r.clients {
		// 排除指定的客户端
		if id == excludeClientID {
			continue
		}

		// 发送消息，忽略错误继续发送给其他客户端
		if err := client.Send(frame); err == nil {
			count++
		} else {
			slog.Debug("failed to send message to client in room",
				"client_id", id,
				"room_id", r.ID,
				"error", err)
		}
	}

	// 记录指标
	metrics.RoomMessageSent()

	slog.Debug("broadcast to room complete",
		"room_id", r.ID,
		"recipients", count,
		"excluded", excludeClientID != "")

	return nil
}

// GetClientCount 获取房间内客户端数量
func (r *Room) GetClientCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}

// HasClient 检查客户端是否在房间内
func (r *Room) HasClient(clientID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.clients[clientID]
	return exists
}

// GetMembers 获取房间内所有客户端ID
func (r *Room) GetMembers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	members := make([]string, 0, len(r.clients))
	for id := range r.clients {
		members = append(members, id)
	}

	return members
}

// RoomManager 管理所有房间
type RoomManager struct {
	rooms        map[string]*Room     // key=roomID
	mu           sync.RWMutex         // 保护rooms映射的互斥锁
	ctx          context.Context      // 用于取消操作的上下文
	bus          bus.MessageBus       // 消息总线，用于集群内房间消息
	deduplicator *MessageDeduplicator // 消息去重器
	nodeID       string               // 节点ID
}

// NewRoomManager 创建一个新的房间管理器
func NewRoomManager(ctx context.Context, messageBus bus.MessageBus) *RoomManager {
	// 生成节点ID（理想情况下应从Hub继承）
	nodeID := fmt.Sprintf("rm-%d", time.Now().UnixNano())

	rm := &RoomManager{
		rooms:        make(map[string]*Room),
		ctx:          ctx,
		bus:          messageBus,
		nodeID:       nodeID,
		deduplicator: NewMessageDeduplicator(nodeID, 30*time.Second),
	}

	// 如果启用了消息总线，设置订阅
	if messageBus != nil {
		rm.setupRoomSubscriptions()
	}

	return rm
}

// setupRoomSubscriptions 设置房间消息订阅
func (rm *RoomManager) setupRoomSubscriptions() {
	// 订阅通配符房间主题
	go func() {
		// 重试参数
		const maxRetries = 5
		const initialBackoff = 1 * time.Second
		const maxBackoff = 30 * time.Second
		retryCount := 0
		backoff := initialBackoff

		for {
			// 订阅所有房间主题的通配符
			roomCh, err := rm.bus.Subscribe(rm.ctx, RoomTopicPrefix+">")
			if err != nil {
				retryCount++
				if retryCount > maxRetries {
					slog.Error("failed to subscribe to room topics after max retries",
						"error", err,
						"attempts", retryCount)
					// 将服务标记为不健康，或者触发告警
					metrics.RecordCriticalError("failed_to_subscribe_room_topics")
					return
				}

				slog.Warn("failed to subscribe to room topics, retrying",
					"error", err,
					"attempt", retryCount,
					"backoff_ms", backoff.Milliseconds())

				// 指数退避重试
				select {
				case <-rm.ctx.Done():
					return
				case <-time.After(backoff):
					// 增加退避时间，但不超过最大值
					backoff = time.Duration(float64(backoff) * 1.5)
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					continue
				}
			}

			// 订阅成功，重置重试计数
			retryCount = 0
			slog.Info("successfully subscribed to room topics")

			// 处理消息
			for {
				select {
				case <-rm.ctx.Done():
					return
				case msg, ok := <-roomCh:
					if !ok {
						slog.Warn("room channel closed, resubscribing")
						break
					}
					// 处理房间消息
					rm.processRoomMessage(msg)
				}
			}
		}
	}()
}

// processRoomMessage 处理从总线接收的房间消息
func (rm *RoomManager) processRoomMessage(data []byte) {
	// 解析BusMessage结构
	var busMsg BusMessage
	if err := json.Unmarshal(data, &busMsg); err != nil {
		slog.Error("failed to unmarshal room message", "error", err)
		return
	}

	// 检查消息是否已处理过（去重）
	msgID := busMsg.ID.String()
	if rm.deduplicator.IsDuplicate(msgID) {
		slog.Debug("ignoring duplicate room message", "id", msgID)
		return
	}

	// 标记消息为已处理
	rm.deduplicator.MarkProcessed(msgID)

	// 检查消息类型
	if busMsg.Type != "room" {
		slog.Warn("received non-room message on room topic", "type", busMsg.Type)
		return
	}

	// 获取房间ID
	roomID := busMsg.RoomID
	if roomID == "" {
		slog.Error("received room message without room_id")
		return
	}

	// 获取房间
	room, err := rm.GetRoom(roomID)
	if err != nil {
		slog.Error("room not found for message", "room_id", roomID, "error", err)
		return
	}

	// 广播消息到房间
	excludeClient := busMsg.ExcludeClient
	if err := room.Broadcast(busMsg.Payload, excludeClient); err != nil {
		slog.Error("failed to broadcast message to room",
			"room_id", roomID,
			"error", err)
	} else {
		slog.Debug("processed room message",
			"id", msgID,
			"room_id", roomID,
			"source_node", busMsg.ID.NodeID)
	}
}

// CreateRoom 创建一个新房间
func (rm *RoomManager) CreateRoom(id, name string, maxClients int) (*Room, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 检查房间是否已存在
	if _, exists := rm.rooms[id]; exists {
		return nil, ErrRoomAlreadyExists
	}

	// 创建新房间
	room := NewRoom(id, name, maxClients)
	rm.rooms[id] = room

	// 记录指标
	metrics.RoomCreated()

	slog.Info("room created", "room_id", id, "room_name", name, "max_clients", maxClients)

	return room, nil
}

// DeleteRoom 删除一个房间
func (rm *RoomManager) DeleteRoom(id string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// 检查房间是否存在
	if _, exists := rm.rooms[id]; !exists {
		return ErrRoomNotFound
	}

	// 删除房间
	delete(rm.rooms, id)

	// 记录指标
	metrics.RoomDeleted()

	slog.Info("room deleted", "room_id", id)

	return nil
}

// GetRoom 获取一个房间
func (rm *RoomManager) GetRoom(id string) (*Room, error) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	room, exists := rm.rooms[id]
	if !exists {
		return nil, ErrRoomNotFound
	}

	return room, nil
}

// ListRooms 列出所有房间
func (rm *RoomManager) ListRooms() []*Room {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	rooms := make([]*Room, 0, len(rm.rooms))
	for _, room := range rm.rooms {
		rooms = append(rooms, room)
	}

	return rooms
}

// JoinRoom 将客户端加入房间
func (rm *RoomManager) JoinRoom(roomID string, client *Client) error {
	// 获取房间
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}

	// 添加客户端到房间
	return room.AddClient(client)
}

// LeaveRoom 让客户端离开房间
func (rm *RoomManager) LeaveRoom(roomID string, clientID string) error {
	// 获取房间
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}

	// 从房间移除客户端
	return room.RemoveClient(clientID)
}

// BroadcastToRoom 向房间内所有客户端广播消息
func (rm *RoomManager) BroadcastToRoom(roomID string, message []byte, excludeClientID string) error {
	// 获取房间
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}

	// 如果启用了消息总线，通过总线发布房间消息
	if rm.bus != nil {
		// 生成消息ID
		msgID := rm.deduplicator.GenerateID()

		// 构造带有元数据的消息
		busMsg := BusMessage{
			ID:            msgID,
			Type:          "room",
			RoomID:        roomID,
			ExcludeClient: excludeClientID,
			Payload:       message,
			SentAt:        time.Now(),
		}

		// 将消息标记为已处理，避免稍后被自己处理
		rm.deduplicator.MarkProcessed(msgID.String())

		// 序列化消息
		msgData, err := json.Marshal(busMsg)
		if err != nil {
			slog.Error("failed to marshal room message", "error", err)
			// 如果序列化失败，仍执行本地房间广播
			return room.Broadcast(message, excludeClientID)
		}

		// 通过总线发布消息
		ctx, cancel := context.WithTimeout(rm.ctx, 500*time.Millisecond)
		defer cancel()

		topic := FormatRoomTopic(roomID)
		if err := rm.bus.Publish(ctx, topic, msgData); err != nil {
			slog.Warn("failed to publish room message via bus", "error", err)
			// 如果总线发布失败，仍执行本地房间广播
			return room.Broadcast(message, excludeClientID)
		}

		// 不再执行本地广播，由processRoomMessage处理
		return nil
	}

	// 没有总线或发布失败，执行本地房间广播
	return room.Broadcast(message, excludeClientID)
}

// ClientInRoom 检查客户端是否在指定房间内
func (rm *RoomManager) ClientInRoom(roomID string, clientID string) (bool, error) {
	// 获取房间
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return false, err
	}

	// 检查客户端是否在房间内
	return room.HasClient(clientID), nil
}

// GetClientRooms 获取客户端加入的所有房间
func (rm *RoomManager) GetClientRooms(clientID string) []string {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	var clientRooms []string
	for id, room := range rm.rooms {
		if room.HasClient(clientID) {
			clientRooms = append(clientRooms, id)
		}
	}

	return clientRooms
}

// Close 关闭房间管理器
func (rm *RoomManager) Close() {
	// 清理所有房间
	rm.mu.Lock()
	defer rm.mu.Unlock()

	roomCount := len(rm.rooms)
	rm.rooms = make(map[string]*Room)

	slog.Info("room manager closed", "rooms_cleaned", roomCount)
}
