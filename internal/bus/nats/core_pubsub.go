package nats

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gohub/internal/bus"

	"github.com/nats-io/nats.go"
)

// 定义错误
var (
	ErrPublishTimeout    = errors.New("publish timeout")
	ErrPublishAckTimeout = errors.New("publish acknowledgement timeout")
)

// Publish 实现MessageBus.Publish，通过NATS发布消息
func (n *NatsBus) Publish(ctx context.Context, topic string, data []byte) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// 检查是否已关闭
	if n.closed {
		return bus.ErrBusClosed
	}

	// 检查主题是否为空
	if topic == "" {
		return bus.ErrTopicEmpty
	}

	// 创建一个带超时的上下文
	publishCtx, cancel := context.WithTimeout(ctx, n.cfg.OpTimeout)
	defer cancel()

	// 发布消息的通道
	errCh := make(chan error, 1)

	// 在goroutine中发布消息，避免阻塞
	go func() {
		var err error
		if n.cfg.UseJetStream && n.js != nil {
			// 记录开始时间，用于计算发布延迟
			startTime := time.Now()

			// 使用JetStream发布，并等待PubAck确认
			ackOpt := nats.AckWait(n.cfg.OpTimeout)
			ack, err := n.js.Publish(topic, data, ackOpt)
			if err != nil {
				n.IncPublishErrors()
				errCh <- err
				return
			}

			// 检查PubAck
			if ack == nil || ack.Sequence == 0 {
				n.IncPublishErrors()
				errCh <- ErrPublishAckTimeout
				return
			}

			// 记录发布延迟
			n.ObservePublishLatency(time.Since(startTime))
		} else {
			// 使用标准NATS发布
			err = n.conn.Publish(topic, data)
			if err != nil {
				n.IncPublishErrors()
			}
		}
		errCh <- err
	}()

	// 等待发布完成或超时
	select {
	case err := <-errCh:
		if err != nil {
			return bus.ErrPublishFailed
		}
		return nil
	case <-publishCtx.Done():
		return ErrPublishTimeout
	}
}

// Subscribe 实现MessageBus.Subscribe，订阅NATS主题
func (n *NatsBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// 检查是否已关闭
	if n.closed {
		return nil, bus.ErrBusClosed
	}

	// 检查主题是否为空
	if topic == "" {
		return nil, bus.ErrTopicEmpty
	}

	// 创建输出通道，使用缓冲区避免阻塞
	outCh := make(chan []byte, 100)

	// 根据配置选择订阅方式
	var sub *nats.Subscription
	var err error

	if n.cfg.UseJetStream && n.js != nil {
		// 使用JetStream订阅
		sub, err = n.subscribeJetStream(topic, outCh)
	} else {
		// 使用标准NATS订阅
		sub, err = n.subscribeStandard(topic, outCh)
	}

	if err != nil {
		close(outCh)
		return nil, err
	}

	// 保存订阅对象，以便后续取消订阅
	n.subs[topic] = sub

	// 启动goroutine监控上下文取消
	go func() {
		<-ctx.Done()
		n.Unsubscribe(topic)
	}()

	// 启动goroutine定期更新Pending消息数量（仅限JetStream）
	if n.cfg.UseJetStream && n.js != nil {
		go n.monitorPendingMessages(sub, topic)
	}

	slog.Info("subscribed to nats topic", "topic", topic)
	return outCh, nil
}

// monitorPendingMessages 定期更新等待确认的消息数量
func (n *NatsBus) monitorPendingMessages(sub *nats.Subscription, topic string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if n.closed {
				return
			}

			// 获取pending消息数量
			delivered, pending, err := sub.Pending()
			if err != nil {
				continue
			}

			// 更新指标
			n.UpdateAckPending(pending)
			slog.Debug("jetstream subscription pending status",
				"topic", topic,
				"delivered", delivered,
				"pending", pending)
		}
	}
}

// subscribeStandard 使用标准NATS订阅
func (n *NatsBus) subscribeStandard(topic string, outCh chan<- []byte) (*nats.Subscription, error) {
	// 使用通道订阅，NATS会自动将消息发送到msgCh
	msgCh := make(chan *nats.Msg, 100)
	sub, err := n.conn.ChanSubscribe(topic, msgCh)
	if err != nil {
		return nil, err
	}

	// 启动goroutine处理接收到的消息
	go func() {
		defer close(outCh)
		for msg := range msgCh {
			// 复制消息数据
			data := make([]byte, len(msg.Data))
			copy(data, msg.Data)

			// 发送到输出通道
			select {
			case outCh <- data:
				// 消息已发送
			case <-time.After(n.cfg.OpTimeout):
				// 如果输出通道阻塞，记录警告
				slog.Warn("timeout sending message to subscriber channel", "topic", topic)
				n.IncSubscribeErrors()
			}
		}
	}()

	return sub, nil
}

// subscribeJetStream 使用JetStream订阅
func (n *NatsBus) subscribeJetStream(topic string, outCh chan<- []byte) (*nats.Subscription, error) {
	// 创建或获取消费者，使用持久化消费者确保消息至少被处理一次
	subOpts := []nats.SubOpt{
		nats.AckExplicit(),               // 手动确认
		nats.Durable(n.cfg.ConsumerName), // 持久化消费者
		nats.DeliverAll(),                // 接收所有可用消息
		nats.MaxDeliver(3),               // 最多重试3次
		nats.AckWait(30 * time.Second),   // 30秒内未确认则重新投递
	}

	// 使用PullSubscribe，让消费者控制获取速率
	sub, err := n.js.PullSubscribe(topic, n.cfg.ConsumerName, subOpts...)
	if err != nil {
		return nil, err
	}

	// 启动goroutine处理接收到的消息
	go func() {
		defer close(outCh)
		for {
			if n.closed {
				return
			}

			// 批量获取消息，每次最多10条，最长等待1秒
			msgs, err := sub.Fetch(10, nats.MaxWait(1*time.Second))
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					// 超时是正常的，继续获取
					continue
				}
				if errors.Is(err, nats.ErrConnectionClosed) || errors.Is(err, nats.ErrDrainTimeout) {
					// 连接已关闭，退出循环
					return
				}
				slog.Error("error fetching messages", "error", err)
				n.IncSubscribeErrors()
				time.Sleep(100 * time.Millisecond)
				continue
			}

			// 处理获取到的消息
			for _, msg := range msgs {
				// 复制消息数据
				data := make([]byte, len(msg.Data))
				copy(data, msg.Data)

				// 记录接收消息的元数据，用于调试
				meta, err := msg.Metadata()
				if err == nil {
					slog.Debug("received jetstream message",
						"topic", topic,
						"sequence", meta.Sequence.Stream,
						"timestamp", meta.Timestamp,
						"redelivered", meta.NumDelivered > 1)
				} else {
					slog.Debug("received jetstream message with no metadata",
						"topic", topic,
						"error", err)
				}

				// 发送到输出通道
				select {
				case outCh <- data:
					// 消息已成功发送到通道，确认消息
					if err := msg.Ack(); err != nil {
						slog.Error("failed to ack message", "error", err)
						n.IncAckErrors()
					}
				case <-time.After(n.cfg.OpTimeout):
					// 如果输出通道阻塞，记录警告并延迟重新投递
					slog.Warn("timeout sending message to subscriber channel", "topic", topic)
					n.IncSubscribeErrors()
					// 尝试稍后重新投递
					if err := msg.NakWithDelay(1 * time.Second); err != nil {
						slog.Error("failed to nak message", "error", err)
						n.IncAckErrors()
					}
				}
			}
		}
	}()

	return sub, nil
}

// Unsubscribe 实现MessageBus.Unsubscribe，取消NATS订阅
func (n *NatsBus) Unsubscribe(topic string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// 检查主题是否为空
	if topic == "" {
		return bus.ErrTopicEmpty
	}

	// 检查是否已关闭
	if n.closed {
		return nil
	}

	// 如果存在该主题的订阅，则取消订阅
	if sub, exists := n.subs[topic]; exists {
		err := sub.Unsubscribe()
		if err != nil {
			return err
		}
		delete(n.subs, topic)
	}

	return nil
}
