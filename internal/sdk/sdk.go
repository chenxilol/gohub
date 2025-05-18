// Package sdk 提供服务端集成接口，方便业务逻辑使用WebSocket功能
package sdk

import (
	"context"
	"gohub/internal/auth"
	"gohub/internal/hub"
	"time"
)

// 客户端事件类型
type EventType string

const (
	EventClientConnected    EventType = "client_connected"    // 客户端连接
	EventClientDisconnected EventType = "client_disconnected" // 客户端断开
	EventClientMessage      EventType = "client_message"      // 客户端消息
	EventClientError        EventType = "client_error"        // 客户端错误
	EventRoomCreated        EventType = "room_created"        // 房间创建
	EventRoomDeleted        EventType = "room_deleted"        // 房间删除
	EventRoomJoined         EventType = "room_joined"         // 加入房间
	EventRoomLeft           EventType = "room_left"           // 离开房间
	EventRoomMessage        EventType = "room_message"        // 房间消息
)

// 客户端事件结构
type Event struct {
	Type     EventType         // 事件类型
	ClientID string            // 客户端ID
	RoomID   string            // 房间ID（如果适用）
	Message  []byte            // 消息内容（如果适用）
	Time     time.Time         // 事件时间
	Claims   *auth.TokenClaims // 认证声明（如果有）
	Extra    map[string]any    // 额外数据
}

// 事件处理器函数类型
type EventHandler func(ctx context.Context, event Event) error

// SDK 服务端接口
type SDK interface {
	// 连接管理
	GetClientCount() int
	GetClient(clientID string) (*hub.Client, error)
	DisconnectClient(clientID string) error
	BroadcastAll(message []byte) error
	SendToClient(clientID string, message []byte) error

	// 房间管理
	CreateRoom(id, name string, maxClients int) error
	DeleteRoom(id string) error
	GetRoom(id string) (*hub.Room, error)
	ListRooms() []*hub.Room
	GetRoomCount() int
	JoinRoom(roomID string, clientID string) error
	LeaveRoom(roomID string, clientID string) error
	BroadcastToRoom(roomID string, message []byte, excludeClientID string) error
	GetRoomMembers(roomID string) ([]string, error)

	// 认证相关
	IsAuthenticated(clientID string) (bool, error)
	GetClientPermissions(clientID string) ([]auth.Permission, error)
	CheckClientPermission(clientID string, permission auth.Permission) (bool, error)

	// 事件订阅
	On(eventType EventType, handler EventHandler)
	Off(eventType EventType, handler EventHandler)

	// 关闭SDK
	Close() error
}

// GoHubSDK 是SDK接口的实现
type GoHubSDK struct {
	hub           *hub.Hub
	eventHandlers map[EventType][]EventHandler
}

// NewSDK 创建新的SDK实例
func NewSDK(h *hub.Hub) *GoHubSDK {
	return &GoHubSDK{
		hub:           h,
		eventHandlers: make(map[EventType][]EventHandler),
	}
}

// 实现SDK接口的连接管理方法

func (s *GoHubSDK) GetClientCount() int {
	return s.hub.GetClientCount()
}

func (s *GoHubSDK) GetClient(clientID string) (*hub.Client, error) {
	// 客户端获取方法需要在hub中实现
	client, err := s.hub.GetClient(clientID)
	if err != nil {
		return nil, err
	}
	return client, nil
}

func (s *GoHubSDK) DisconnectClient(clientID string) error {
	// 断开连接方法需要在hub中实现
	client, err := s.hub.GetClient(clientID)
	if err != nil {
		return err
	}

	// 关闭客户端连接
	client.Shutdown()
	return nil
}

func (s *GoHubSDK) BroadcastAll(message []byte) error {
	s.hub.Broadcast(hub.Frame{
		MsgType: 1, // 文本消息
		Data:    message,
	})
	return nil
}

func (s *GoHubSDK) SendToClient(clientID string, message []byte) error {
	return s.hub.Push(clientID, hub.Frame{
		MsgType: 1, // 文本消息
		Data:    message,
	})
}

// 实现SDK接口的房间管理方法

func (s *GoHubSDK) CreateRoom(id, name string, maxClients int) error {
	_, err := s.hub.CreateRoom(id, name, maxClients)
	return err
}

func (s *GoHubSDK) DeleteRoom(id string) error {
	return s.hub.DeleteRoom(id)
}

func (s *GoHubSDK) GetRoom(id string) (*hub.Room, error) {
	return s.hub.GetRoom(id)
}

func (s *GoHubSDK) ListRooms() []*hub.Room {
	return s.hub.ListRooms()
}

func (s *GoHubSDK) GetRoomCount() int {
	return len(s.hub.ListRooms())
}

func (s *GoHubSDK) JoinRoom(roomID string, clientID string) error {
	return s.hub.JoinRoom(roomID, clientID)
}

func (s *GoHubSDK) LeaveRoom(roomID string, clientID string) error {
	return s.hub.LeaveRoom(roomID, clientID)
}

func (s *GoHubSDK) BroadcastToRoom(roomID string, message []byte, excludeClientID string) error {
	return s.hub.BroadcastToRoom(roomID, hub.Frame{
		MsgType: 1, // 文本消息
		Data:    message,
	}, excludeClientID)
}

func (s *GoHubSDK) GetRoomMembers(roomID string) ([]string, error) {
	room, err := s.hub.GetRoom(roomID)
	if err != nil {
		return nil, err
	}

	return room.GetMembers(), nil
}

// 实现SDK接口的认证相关方法

func (s *GoHubSDK) IsAuthenticated(clientID string) (bool, error) {
	client, err := s.hub.GetClient(clientID)
	if err != nil {
		return false, err
	}

	return client.IsAuthenticated(), nil
}

func (s *GoHubSDK) GetClientPermissions(clientID string) ([]auth.Permission, error) {
	client, err := s.hub.GetClient(clientID)
	if err != nil {
		return nil, err
	}

	claims := client.GetAuthClaims()
	if claims == nil {
		return nil, nil
	}

	return claims.Permissions, nil
}

func (s *GoHubSDK) CheckClientPermission(clientID string, permission auth.Permission) (bool, error) {
	client, err := s.hub.GetClient(clientID)
	if err != nil {
		return false, err
	}

	return client.HasPermission(permission), nil
}

// 实现SDK接口的事件订阅方法

func (s *GoHubSDK) On(eventType EventType, handler EventHandler) {
	s.eventHandlers[eventType] = append(s.eventHandlers[eventType], handler)
}

func (s *GoHubSDK) Off(eventType EventType, handler EventHandler) {
	handlers, ok := s.eventHandlers[eventType]
	if !ok {
		return
	}

	// 移除指定的处理器
	var newHandlers []EventHandler
	for _, h := range handlers {
		if &h != &handler { // 简单比较指针，实际中可能需要更复杂的比较
			newHandlers = append(newHandlers, h)
		}
	}

	s.eventHandlers[eventType] = newHandlers
}

// 触发事件（内部使用）
func (s *GoHubSDK) triggerEvent(ctx context.Context, event Event) {
	handlers, ok := s.eventHandlers[event.Type]
	if !ok {
		return
	}

	for _, handler := range handlers {
		err := handler(ctx, event)
		if err != nil {
			// 记录错误但继续执行其他处理器
			// TODO: 使用slog记录错误
		}
	}
}

// TriggerEvent 触发事件（外部可用）
func (s *GoHubSDK) TriggerEvent(ctx context.Context, event Event) {
	s.triggerEvent(ctx, event)
}

// Close 实现SDK接口的关闭方法
func (s *GoHubSDK) Close() error {
	// 清理事件处理器
	s.eventHandlers = make(map[EventType][]EventHandler)

	// Hub的关闭由外部控制，这里不主动关闭
	return nil
}
