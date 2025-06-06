// Package handlers 提供WebSocket消息处理函数
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"

	"time"

	"github.com/chenxilol/gohub/internal/metrics"
	"github.com/chenxilol/gohub/pkg/auth"
	"github.com/chenxilol/gohub/pkg/hub"
)

// 消息类型常量
const (
	MsgTypePing        = "ping"
	MsgTypeJoinRoom    = "join_room"
	MsgTypeLeaveRoom   = "leave_room"
	MsgTypeRoomMessage = "room_message"
	MsgTypeDirect      = "direct_message"
)

// RegisterHandlers 注册所有消息处理函数到分发器
func RegisterHandlers(d hub.MessageDispatcher) {
	d.Register(MsgTypePing, handlePing)
	d.Register(MsgTypeJoinRoom, handleJoinRoom)
	d.Register(MsgTypeLeaveRoom, handleLeaveRoom)
	d.Register(MsgTypeRoomMessage, handleRoomMessage)
	d.Register(MsgTypeDirect, handleDirectMessage)

	slog.Info("registered all message handlers")
}

// 处理ping消息，用于心跳检测
func handlePing(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	// 解析ping数据
	var pingData struct {
		Timestamp int64 `json:"ts"`
	}

	if err := json.Unmarshal(data, &pingData); err != nil {
		return err
	}

	// 计算延迟
	now := time.Now().UnixNano() / int64(time.Millisecond)
	latency := float64(now - pingData.Timestamp)

	// 记录延迟指标
	metrics.RecordLatency(latency)

	// 构造pong响应
	pongData := struct {
		Timestamp int64 `json:"ts"`
		ServerTS  int64 `json:"server_ts"`
		RTT       int64 `json:"rtt"`
	}{
		Timestamp: pingData.Timestamp,
		ServerTS:  now,
		RTT:       now - pingData.Timestamp,
	}

	pongMsg, err := hub.NewMessage(0, "pong", pongData)
	if err != nil {
		return err
	}

	return client.Send(hub.Frame{
		MsgType: 1,
		Data:    pongMsg.Encode(),
	})
}

// 处理加入房间消息
func handleJoinRoom(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	// 解析房间加入请求
	var joinRequest struct {
		RoomID string `json:"room_id"`
	}

	if err := json.Unmarshal(data, &joinRequest); err != nil {
		return err
	}

	// 检查权限
	if !client.HasPermission(auth.PermJoinRoom) {
		// 发送错误响应
		errorResp := hub.NewError(hub.ErrCodeForbidden, "You don't have permission to join rooms", 0, nil)
		return hub.SendError(client, errorResp)
	}

	// 获取客户端上下文中的Hub实例（需要实现获取Hub的方法）
	h, ok := ctx.Value("hub").(*hub.Hub)
	if !ok {
		slog.Error("failed to get hub from context")
		return hub.NewError(hub.ErrCodeServerError, "Internal server error", 0, nil)
	}

	// 加入房间
	if err := h.JoinRoom(joinRequest.RoomID, client.GetID()); err != nil {
		// 处理错误
		code := hub.MapErrorToCode(err)
		return hub.SendError(client, hub.NewError(code, err.Error(), 0, nil))
	}

	// 发送成功响应
	successMsg, _ := hub.NewMessage(0, "room_joined", map[string]string{
		"room_id": joinRequest.RoomID,
		"status":  "joined",
	})

	return client.Send(hub.Frame{
		MsgType: 1,
		Data:    successMsg.Encode(),
	})
}

// 处理离开房间消息
func handleLeaveRoom(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	// 解析离开房间请求
	var leaveRequest struct {
		RoomID string `json:"room_id"`
	}

	if err := json.Unmarshal(data, &leaveRequest); err != nil {
		return err
	}

	// 获取Hub
	h, ok := ctx.Value("hub").(*hub.Hub)
	if !ok {
		slog.Error("failed to get hub from context")
		return hub.NewError(hub.ErrCodeServerError, "Internal server error", 0, nil)
	}

	// 离开房间
	if err := h.LeaveRoom(leaveRequest.RoomID, client.GetID()); err != nil {
		// 处理错误
		code := hub.MapErrorToCode(err)
		return hub.SendError(client, hub.NewError(code, err.Error(), 0, nil))
	}

	// 发送成功响应
	successMsg, _ := hub.NewMessage(0, "room_left", map[string]string{
		"room_id": leaveRequest.RoomID,
		"status":  "left",
	})

	return client.Send(hub.Frame{
		MsgType: 1,
		Data:    successMsg.Encode(),
	})
}

// 处理房间消息
func handleRoomMessage(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	// 解析房间消息请求
	var roomMsgRequest struct {
		RoomID  string          `json:"room_id"`
		Content json.RawMessage `json:"content"`
	}

	if err := json.Unmarshal(data, &roomMsgRequest); err != nil {
		return err
	}

	// 检查发送消息权限
	if !client.HasPermission(auth.PermSendMessage) {
		errorResp := hub.NewError(hub.ErrCodeForbidden, "You don't have permission to send messages", 0, nil)
		return hub.SendError(client, errorResp)
	}

	// 获取Hub
	h, ok := ctx.Value("hub").(*hub.Hub)
	if !ok {
		slog.Error("failed to get hub from context")
		return hub.NewError(hub.ErrCodeServerError, "Internal server error", 0, nil)
	}

	// 构造房间消息
	roomMsg, err := hub.NewMessage(0, "room_message", map[string]interface{}{
		"room_id":   roomMsgRequest.RoomID,
		"content":   roomMsgRequest.Content,
		"sender_id": client.GetID(),
		"timestamp": time.Now().UnixNano() / int64(time.Millisecond),
	})
	if err != nil {
		return err
	}

	// 记录指标
	metrics.MessageSent(float64(len(roomMsg.Encode())))
	metrics.RoomMessageSent()

	// 广播消息到房间（排除发送者）
	return h.BroadcastToRoom(roomMsgRequest.RoomID, hub.Frame{
		MsgType: 1,
		Data:    roomMsg.Encode(),
	}, client.GetID())
}

// 处理私信
func handleDirectMessage(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	// 解析私信请求
	var dmRequest struct {
		TargetID string          `json:"target_id"`
		Content  json.RawMessage `json:"content"`
	}

	if err := json.Unmarshal(data, &dmRequest); err != nil {
		return err
	}

	// 检查发送消息权限
	if !client.HasPermission(auth.PermSendMessage) {
		errorResp := hub.NewError(hub.ErrCodeForbidden, "You don't have permission to send messages", 0, nil)
		return hub.SendError(client, errorResp)
	}

	// 获取Hub
	h, ok := ctx.Value("hub").(*hub.Hub)
	if !ok {
		slog.Error("failed to get hub from context")
		return hub.NewError(hub.ErrCodeServerError, "Internal server error", 0, nil)
	}

	// 构造私信消息
	directMsg, err := hub.NewMessage(0, "direct_message", map[string]interface{}{
		"content":   dmRequest.Content,
		"sender_id": client.GetID(),
		"timestamp": time.Now().UnixNano() / int64(time.Millisecond),
	})
	if err != nil {
		return err
	}

	// 记录指标
	metrics.MessageSent(float64(len(directMsg.Encode())))

	// 发送消息到目标客户端
	if err := h.
		Push(dmRequest.TargetID, hub.Frame{
			MsgType: 1,
			Data:    directMsg.Encode(),
		}); err != nil {
		// 处理错误
		code := hub.MapErrorToCode(err)
		return hub.SendError(client, hub.NewError(code, "Failed to deliver message: "+err.Error(), 0, nil))
	}

	return nil
}
