// Package noop 提供一个空操作的消息总线实现
package noop

import (
	"context"
	"sync"

	"gohub/internal/bus"
)

// NoopBus 是一个空操作的消息总线实现，直接丢弃消息
// 适用于单节点模式，不需要跨节点通信
type NoopBus struct {
	closed bool
	mu     sync.Mutex
}

// New 创建一个新的NoopBus实例
func New() *NoopBus {
	return &NoopBus{}
}

// Publish 实现MessageBus.Publish，实际上不做任何事情，直接返回nil
func (n *NoopBus) Publish(ctx context.Context, topic string, data []byte) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return bus.ErrBusClosed
	}

	if topic == "" {
		return bus.ErrTopicEmpty
	}

	// 直接丢弃消息，不做处理
	return nil
}

// Subscribe 实现MessageBus.Subscribe，返回一个立即关闭的通道
func (n *NoopBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil, bus.ErrBusClosed
	}

	if topic == "" {
		return nil, bus.ErrTopicEmpty
	}

	// 创建一个非关闭的通道，不会有消息被发送到这个通道
	// 返回未关闭的通道，避免接收方在遍历通道时立即结束
	ch := make(chan []byte)
	return ch, nil
}

// Unsubscribe 实现MessageBus.Unsubscribe，不做任何操作
func (n *NoopBus) Unsubscribe(topic string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if topic == "" {
		return bus.ErrTopicEmpty
	}

	// 不需要做任何事情
	return nil
}

// Close 实现MessageBus.Close，标记为已关闭
func (n *NoopBus) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil
	}

	n.closed = true
	return nil
}

// 确保NoopBus实现了MessageBus接口
var _ bus.MessageBus = (*NoopBus)(nil)
