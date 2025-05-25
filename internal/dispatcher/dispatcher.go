// Package dispatcher 提供消息路由和处理功能
package dispatcher

import (
	"context"
	"encoding/json"
	"errors"
	"gohub/internal/hub"
	"log/slog"
	"sync"
)

var (
	ErrUnknownMessageType = errors.New("unknown message type")
	ErrInvalidFormat      = errors.New("invalid message format")
	ErrHandlerNotFound    = errors.New("handler not found")
)

type HandlerFunc func(ctx context.Context, client *hub.Client, data json.RawMessage) error

type Dispatcher struct {
	handlers map[string]HandlerFunc
	mu       sync.RWMutex
}

var (
	instance *Dispatcher
	once     sync.Once
)

func GetDispatcher() *Dispatcher {
	once.Do(func() {
		instance = NewDispatcher()
	})
	return instance
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		handlers: make(map[string]HandlerFunc),
	}
}

func (d *Dispatcher) Register(msgType string, handler HandlerFunc) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[msgType] = handler
	slog.Debug("registered message handler", "type", msgType)
}

func (d *Dispatcher) DecodeAndRoute(ctx context.Context, client *hub.Client, message []byte) error {
	var msg hub.Message
	if err := json.Unmarshal(message, &msg); err != nil {
		slog.Error("failed to decode message", "error", err, "client", client.ID())
		return ErrInvalidFormat
	}

	// 根据消息类型路由到对应处理函数
	d.mu.RLock()
	handler, exists := d.handlers[msg.MessageType]
	d.mu.RUnlock()

	if !exists {
		slog.Error("unknown message type", "type", msg.MessageType, "client", client.ID())
		return ErrUnknownMessageType
	}

	// 处理消息
	err := handler(ctx, client, msg.Data)
	if err != nil {
		slog.Error("failed to handle message", "type", msg.MessageType, "client", client.ID(), "error", err)
		return err
	}

	return nil
}

var _ hub.MessageDispatcher = (*Dispatcher)(nil)
