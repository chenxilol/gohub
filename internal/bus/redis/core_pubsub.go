package redis

import (
	"context"

	"gohub/internal/bus"
)

func (r *RedisBus) Publish(ctx context.Context, topic string, data []byte) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.closed {
		return bus.ErrBusClosed
	}

	if topic == "" {
		return bus.ErrTopicEmpty
	}

	msg := bus.NewMessage(data)
	msgData, err := msg.Marshal()
	if err != nil {
		r.IncPublishErrors()
		return bus.ErrPublishFailed
	}

	publishCtx, cancel := context.WithTimeout(ctx, r.cfg.OpTimeout)
	defer cancel()

	formattedTopic := r.formatKey(topic)
	cmd := r.client.Publish(publishCtx, formattedTopic, msgData)
	if err := cmd.Err(); err != nil {
		r.IncPublishErrors()
		return bus.ErrPublishFailed
	}

	// PUBLISH命令返回接收消息的客户端数量
	// 这可以用来确认是否有订阅者
	// 但在Redis PubSub模型中，没有订阅者也是正常的，不应视为错误
	return nil
}

// Unsubscribe 实现MessageBus.Unsubscribe，取消Redis订阅
func (r *RedisBus) Unsubscribe(topic string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查主题是否为空
	if topic == "" {
		return bus.ErrTopicEmpty
	}

	// 检查是否已关闭
	if r.closed {
		return nil
	}

	// 如果存在该主题的取消函数，则调用它
	if cancel, exists := r.subs[topic]; exists && cancel != nil {
		cancel() // 取消上下文，goroutine会自行退出
		delete(r.subs, topic)
	}

	return nil
}

// Subscribe 实现MessageBus.Subscribe，订阅Redis频道
func (r *RedisBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查是否已关闭
	if r.closed {
		return nil, bus.ErrBusClosed
	}

	// 检查主题是否为空
	if topic == "" {
		return nil, bus.ErrTopicEmpty
	}

	// 格式化主题名称
	formattedTopic := r.formatKey(topic)

	// 创建输出通道，使用缓冲区避免阻塞
	outCh := make(chan []byte, 100)

	// 创建一个可取消的上下文，用于控制订阅goroutine的生命周期
	subCtx, cancel := context.WithCancel(ctx)

	// 记录取消函数，以便后续可以取消订阅
	r.subs[topic] = cancel

	// 启动后台goroutine处理订阅
	go r.subscribeRoutine(subCtx, formattedTopic, outCh)

	return outCh, nil
}

// SubscribeWithTimestamp 实现MessageBus.SubscribeWithTimestamp，订阅Redis频道并返回带时间戳的消息
func (r *RedisBus) SubscribeWithTimestamp(ctx context.Context, topic string) (<-chan *bus.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 检查是否已关闭
	if r.closed {
		return nil, bus.ErrBusClosed
	}

	// 检查主题是否为空
	if topic == "" {
		return nil, bus.ErrTopicEmpty
	}

	// 格式化主题名称
	formattedTopic := r.formatKey(topic)

	// 创建输出通道，使用缓冲区避免阻塞
	outCh := make(chan *bus.Message, 100)

	// 创建一个可取消的上下文，用于控制订阅goroutine的生命周期
	subCtx, cancel := context.WithCancel(ctx)

	// 记录取消函数，以便后续可以取消订阅
	r.subs[topic+"_timestamp"] = cancel

	// 启动后台goroutine处理订阅
	go r.subscribeRoutineWithTimestamp(subCtx, formattedTopic, outCh)

	return outCh, nil
}
