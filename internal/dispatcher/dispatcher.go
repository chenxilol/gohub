// Package dispatcher 提供WebSocket消息路由和处理机制
package dispatcher

import (
	"context"
	"encoding/json"
	"gohub/internal/hub"
	"log/slog"
	"sync"
)

// Handler 是消息处理函数类型
type Handler func(context.Context, *hub.Client, hub.Message) error

// Dispatcher 消息分发器，根据消息ID路由到对应的处理函数
type Dispatcher struct {
	table map[int]Handler
	mu    sync.RWMutex
}

// NewDispatcher 创建新的分发器
func NewDispatcher() *Dispatcher {
	return &Dispatcher{
		table: make(map[int]Handler),
	}
}

// Register 注册消息处理函数
func (d *Dispatcher) Register(id int, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.table[id] = h
}

// DecodeAndRoute 解码消息并路由到对应的处理函数
func (d *Dispatcher) DecodeAndRoute(ctx context.Context, c *hub.Client, data []byte) error {
	var m hub.Message
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}

	d.mu.RLock()
	handler, exists := d.table[m.ID]
	d.mu.RUnlock()

	if !exists {
		slog.Debug("no handler registered for message", "message_id", m.ID)
		return nil
	}

	return handler(ctx, c, m)
}

// 全局单例分发器
var dispatcherSingleton = NewDispatcher()

// GetDispatcher 获取全局分发器实例
func GetDispatcher() *Dispatcher {
	return dispatcherSingleton
}
