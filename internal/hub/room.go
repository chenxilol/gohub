package hub

import (
	"context"
	"errors"
	"gohub/internal/bus"
	"gohub/internal/metrics"
	"log/slog"
	"sync"
)

// 定义房间相关错误
var (
	ErrRoomNotFound        = errors.New("room not found")
	ErrRoomAlreadyExists   = errors.New("room already exists")
	ErrRoomIsFull          = errors.New("room is full")
	ErrClientNotInRoom     = errors.New("client not in room")
	ErrClientAlreadyInRoom = errors.New("client already in room")
)

// 格式化房间主题
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
	rooms map[string]*Room // key=roomID
	mu    sync.RWMutex     // 保护rooms映射的互斥锁
	ctx   context.Context  // 用于取消操作的上下文
	bus   bus.MessageBus   // 消息总线，用于集群内房间消息
}

// NewRoomManager 创建一个新的房间管理器
func NewRoomManager(ctx context.Context, messageBus bus.MessageBus) *RoomManager {
	rm := &RoomManager{
		rooms: make(map[string]*Room),
		ctx:   ctx,
		bus:   messageBus,
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
		// 订阅所有房间主题的通配符
		roomCh, err := rm.bus.Subscribe(rm.ctx, RoomTopicPrefix+">")
		if err != nil {
			slog.Error("failed to subscribe to room topics", "error", err)
			return
		}

		for {
			select {
			case <-rm.ctx.Done():
				return
			case msg, ok := <-roomCh:
				if !ok {
					return
				}
				// 处理房间消息
				rm.processRoomMessage(msg)
			}
		}
	}()
}

// processRoomMessage 处理从总线接收的房间消息
func (rm *RoomManager) processRoomMessage(data []byte) {
	// 解析房间消息
	// 简单实现，实际应该使用json.Unmarshal解析完整的消息格式
	slog.Debug("received room message from bus", "data_len", len(data))

	// TODO: 完整的消息解析和处理
	// 现在仅记录收到消息
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

	// 向房间广播消息
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
