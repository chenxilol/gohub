// Package handlers 提供WebSocket消息处理函数
package handlers

import (
	"context"
	"gohub/internal/dispatcher"
	hub2 "gohub/internal/hub"
	"log/slog"
	"time"

	"github.com/gorilla/websocket"
)

// MsgPingID 常量定义
const (
	MsgPingID = 1
)

// PingHandler 处理Ping消息
func PingHandler(ctx context.Context, c *hub2.Client, m hub2.Message) error {
	slog.Debug("ping received", "client", c.ID())

	// 创建Pong响应
	pong, err := hub2.NewMessage(MsgPingID, "pong", map[string]any{
		"ts": time.Now().UnixNano() / int64(time.Millisecond),
	})
	if err != nil {
		return err
	}

	// 序列化消息
	data, err := pong.Encode()
	if err != nil {
		return err
	}

	// 发送响应
	return c.Send(hub2.Frame{
		MsgType: websocket.TextMessage,
		Data:    data,
	})
}

// RegisterHandlers 注册所有处理函数到分发器
func RegisterHandlers(d *dispatcher.Dispatcher) {
	d.Register(MsgPingID, PingHandler)
}
