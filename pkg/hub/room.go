package hub

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/chenxilol/gohub/internal/metrics"
	"github.com/chenxilol/gohub/internal/utils"
	"github.com/chenxilol/gohub/pkg/bus"

	"github.com/gorilla/websocket"
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

	if r.MaxClients > 0 && len(r.clients) >= r.MaxClients {
		return ErrRoomIsFull
	}
	if _, exists := r.clients[client.ID()]; exists {
		return ErrClientAlreadyInRoom
	}
	r.clients[client.ID()] = client
	metrics.RoomJoined()
	slog.Info("client joined room", "client_id", client.ID(), "room_id", r.ID, "room_name", r.Name, "total_clients", len(r.clients))
	return nil
}

// RemoveClient 从房间移除客户端
func (r *Room) RemoveClient(clientID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.clients[clientID]; !exists {
		return ErrClientNotInRoom
	}
	delete(r.clients, clientID)
	metrics.RoomLeft()
	slog.Info("client left room", "client_id", clientID, "room_id", r.ID, "room_name", r.Name, "remaining_clients", len(r.clients))
	return nil
}

// Broadcast 向房间内所有客户端广播消息
func (r *Room) Broadcast(message []byte, excludeClientID string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	frame := Frame{
		MsgType: websocket.TextMessage,
		Data:    message,
	}

	count := 0
	for id, client := range r.clients {
		if id == excludeClientID {
			continue
		}
		if err := client.Send(frame); err != nil {
			slog.Debug("failed to send message to client in room", "client_id", id, "room_id", r.ID, "error", err)
			continue
		}
		count++
	}
	metrics.RoomMessageSent()
	slog.Debug("broadcast to room complete", "room_id", r.ID, "recipients", count, "excluded", excludeClientID != "")
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

type RoomManager struct {
	rooms        map[string]*Room
	mu           sync.RWMutex
	ctx          context.Context
	bus          bus.MessageBus
	deduplicator *MessageDeduplicator
}

// NewRoomManager 创建一个新的房间管理器
func NewRoomManager(ctx context.Context, messageBus bus.MessageBus, dedup *MessageDeduplicator) *RoomManager {
	if dedup == nil || messageBus == nil {
		slog.Error("deduplicator or message bus is nil, room message deduplication will not work correctly")
		return nil
	}

	rm := &RoomManager{
		rooms:        make(map[string]*Room),
		ctx:          ctx,
		bus:          messageBus,
		deduplicator: dedup,
	}

	rm.setupRoomSubscriptions()

	return rm
}

// setupRoomSubscriptions 设置房间消息订阅
func (rm *RoomManager) setupRoomSubscriptions() {
	go func() {
		var roomCh <-chan []byte
		var err error
		subscribeOperation := func() error {
			// RoomTopicPrefix+">" 订阅所有以 "room/" 开头的主题
			roomCh, err = rm.bus.Subscribe(rm.ctx, RoomTopicPrefix+">")
			return err
		}

		for {
			select {
			case <-rm.ctx.Done():
				slog.InfoContext(rm.ctx, "RoomManager context done, stopping room topics subscription routine")
				return
			default:
			}

			if err := utils.RetryWithBackoff(rm.ctx, "room_bus_subscribe", 5, time.Second, time.Minute, subscribeOperation); err != nil {
				slog.ErrorContext(rm.ctx, "RoomManager failed to subscribe to room topics after multiple retries", "error", err)
				metrics.RecordCriticalError("failed_to_subscribe_room_topics")
				return
			}

			if roomCh == nil && err == nil {
				slog.ErrorContext(rm.ctx, "RoomManager: room bus subscribe returned nil channel without error")
				time.Sleep(1 * time.Second)
				continue
			}

			if processingErr := rm.processRoomMessagesLoop(roomCh); processingErr != nil {
				if errors.Is(processingErr, context.Canceled) || errors.Is(processingErr, context.DeadlineExceeded) {
					slog.InfoContext(rm.ctx, "RoomManager: room message processing stopped due to context completion", "reason", processingErr)
					return
				}
			}

			time.Sleep(100 * time.Millisecond)
		}
	}()
}

// processRoomMessagesLoop 处理房间消息循环
func (rm *RoomManager) processRoomMessagesLoop(roomCh <-chan []byte) error {
	for {
		select {
		case <-rm.ctx.Done():
			return rm.ctx.Err()
		case msgData, ok := <-roomCh:
			if !ok {
				return errors.New("room channel closed")
			}
			rm.processRoomMessage(msgData)
		}
	}
}

// processRoomMessage 处理从bus接收的房间消息
func (rm *RoomManager) processRoomMessage(data []byte) {
	var busMsg BusMessage
	if err := json.Unmarshal(data, &busMsg); err != nil {
		slog.ErrorContext(rm.ctx, "RoomManager failed to unmarshal room message from bus", "error", err, "raw_data", string(data))
		return
	}

	msgIDStr := busMsg.ID.String()
	if rm.deduplicator.IsDuplicate(msgIDStr) {
		slog.DebugContext(rm.ctx, "RoomManager ignoring duplicate room message from bus", "id", msgIDStr, "room_id", busMsg.RoomID)
		return
	}

	if busMsg.Type != "room" { // 确保与发布时使用的类型一致
		slog.WarnContext(rm.ctx, "RoomManager received non-room message type on room topic", "type", busMsg.Type, "room_id", busMsg.RoomID)
		return
	}

	if busMsg.RoomID == "" {
		slog.ErrorContext(rm.ctx, "RoomManager received room message from bus without room_id")
		return
	}

	room, err := rm.GetRoom(busMsg.RoomID)
	if err != nil {
		slog.DebugContext(rm.ctx, "RoomManager: room not found for bus message", "room_id", busMsg.RoomID, "error", err)
		return
	}

	// 广播消息到房间内的本地客户端
	if err := room.Broadcast(busMsg.Payload, busMsg.ExcludeClient); err != nil {
		slog.ErrorContext(rm.ctx, "RoomManager failed to broadcast bus message to local room clients", "room_id", busMsg.RoomID, "error", err)
		return
	}
	rm.deduplicator.MarkProcessed(msgIDStr)
}

// CreateRoom 创建一个新房间
func (rm *RoomManager) CreateRoom(id, name string, maxClients int) (*Room, error) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if _, exists := rm.rooms[id]; exists {
		return nil, ErrRoomAlreadyExists
	}
	room := NewRoom(id, name, maxClients)
	rm.rooms[id] = room
	metrics.RoomCreated()
	slog.Info("room created", "room_id", id, "room_name", name, "max_clients", maxClients)

	return room, nil
}

// DeleteRoom 删除一个房间
func (rm *RoomManager) DeleteRoom(id string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if _, exists := rm.rooms[id]; !exists {
		return ErrRoomNotFound
	}
	delete(rm.rooms, id)
	metrics.RoomDeleted()
	slog.InfoContext(rm.ctx, "Room deleted by RoomManager", "room_id", id)
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
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}
	return room.AddClient(client)
}

// LeaveRoom 让客户端离开房间
func (rm *RoomManager) LeaveRoom(roomID string, clientID string) error {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}
	return room.RemoveClient(clientID)
}

// BroadcastToRoom 向房间内所有客户端广播消息
func (rm *RoomManager) BroadcastToRoom(roomID string, message []byte, excludeClientID string) error {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return err
	}
	if rm.bus != nil {
		msgID := rm.deduplicator.GenerateID()
		busMsg := BusMessage{
			ID:            msgID,
			Type:          "room",
			RoomID:        roomID,
			ExcludeClient: excludeClientID,
			Payload:       message,
			SentAt:        time.Now(),
		}
		rm.deduplicator.MarkProcessed(msgID.String())
		msgData, err := json.Marshal(busMsg)
		if err != nil {
			slog.Error("failed to marshal room message", "error", err)
			return err
		}
		ctx, cancel := context.WithTimeout(rm.ctx, 500*time.Millisecond)
		defer cancel()
		topic := FormatRoomTopic(roomID)
		if err := rm.bus.Publish(ctx, topic, msgData); err != nil {
			slog.Warn("failed to publish room message via bus", "error", err)
			return err
		}
		return nil
	}

	// 没有总线或发布失败，执行本地房间广播
	return room.Broadcast(message, excludeClientID)
}

// ClientInRoom 检查客户端是否在指定房间内
func (rm *RoomManager) ClientInRoom(roomID string, clientID string) (bool, error) {
	room, err := rm.GetRoom(roomID)
	if err != nil {
		return false, err
	}
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
	rm.mu.Lock()
	defer rm.mu.Unlock()
	roomCount := len(rm.rooms)
	rm.rooms = make(map[string]*Room)
	slog.Info("room manager closed", "rooms_cleaned", roomCount)
}
