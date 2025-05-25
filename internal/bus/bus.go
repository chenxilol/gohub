// Package bus 提供节点间消息传递机制
package bus

import (
	"context"
	"errors"
)

// 定义错误类型
var (
	ErrTopicEmpty    = errors.New("topic cannot be empty")
	ErrBusClosed     = errors.New("message bus is closed")
	ErrPublishFailed = errors.New("publish message failed")
)

// MessageBus 在节点间传播消息
type MessageBus interface {
	// Publish 发布消息到指定主题
	Publish(ctx context.Context, topic string, data []byte) error

	// Subscribe 订阅指定主题，返回接收channel
	Subscribe(ctx context.Context, topic string) (<-chan []byte, error)

	// SubscribeWithTimestamp 订阅指定主题，返回带时间戳的消息channel
	SubscribeWithTimestamp(ctx context.Context, topic string) (<-chan *Message, error)

	// Unsubscribe 取消订阅主题
	Unsubscribe(topic string) error

	Close() error
}

// 主题常量
const (
	BroadcastTopic = "broadcast" // 广播消息
	UnicastPrefix  = "unicast/"  // 单播前缀
)

// FormatUnicastTopic 格式化单播主题
func FormatUnicastTopic(clientID string) string {
	return UnicastPrefix + clientID
}
