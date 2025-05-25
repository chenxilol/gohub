package nats

import (
	"context"
	"errors"
	"fmt"
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

func (n *NatsBus) Publish(ctx context.Context, topic string, data []byte) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.closed {
		return bus.ErrBusClosed
	}

	if topic == "" {
		return bus.ErrTopicEmpty
	}

	msg := bus.NewMessage(data)
	msgData, err := msg.Marshal()
	if err != nil {
		n.IncPublishErrors()
		return bus.ErrPublishFailed
	}

	opTimeout := n.cfg.OpTimeout
	if n.cfg.UseJetStream && n.js != nil {
		opTimeout = 5 * time.Second
	}
	publishCtx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()

	errCh := make(chan error, 1)

	go func() {
		var err error
		if n.cfg.UseJetStream && n.js != nil {
			fullTopic := topic
			if n.streamSubject != "" {
				fullTopic = n.streamSubject + "." + topic
			}

			startTime := time.Now()

			var ack *nats.PubAck
			var innerErr error
			maxRetries := 8 // 增加重试次数
			baseBackoff := 50 * time.Millisecond
			maxBackoff := 1 * time.Second

			for i := 0; i < maxRetries; i++ {
				// 为JetStream发布设置足够长的确认等待时间
				ackOpt := nats.AckWait(2 * time.Second) // 增加确认等待时间

				slog.Debug("attempting jetstream publish", "attempt", i+1, "topic", fullTopic)
				ack, innerErr = n.js.Publish(fullTopic, msgData, ackOpt)
				if innerErr == nil && ack != nil && ack.Sequence > 0 {
					slog.Debug("jetstream publish successful",
						"sequence", ack.Sequence,
						"stream", ack.Stream,
						"topic", fullTopic)
					break
				}

				if innerErr != nil {
					slog.Debug("retrying jetstream publish", "attempt", i+1, "error", innerErr)
				} else if ack == nil || ack.Sequence == 0 {
					slog.Debug("retrying jetstream publish due to invalid ack", "attempt", i+1)
				}

				// 随着重试次数增加，延迟时间也增加（指数退避）
				backoff := time.Duration(baseBackoff.Nanoseconds() * (1 << i))
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				time.Sleep(backoff)
			}

			if innerErr != nil {
				n.IncPublishErrors()
				slog.Error("jetstream publish failed after retries", "error", innerErr, "topic", fullTopic)
				err = fmt.Errorf("%w: %s", ErrPublishAckTimeout, innerErr.Error())
			} else if ack == nil || ack.Sequence == 0 {
				n.IncPublishErrors()
				slog.Error("jetstream publish failed with invalid ack", "topic", fullTopic)
				err = ErrPublishAckTimeout
			} else {
				// 记录发布延迟
				latency := time.Since(startTime)
				n.ObservePublishLatency(latency)
				slog.Debug("jetstream publish completed",
					"latency_ms", latency.Milliseconds(),
					"topic", fullTopic)
			}
		} else {
			// 使用标准NATS发布
			err = n.conn.Publish(topic, msgData)
			if err != nil {
				n.IncPublishErrors()
				slog.Error("standard nats publish failed", "error", err, "topic", topic)
			}
		}
		errCh <- err
	}()

	// 等待发布完成或超时
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("%w: %v", bus.ErrPublishFailed, err)
		}
		return nil
	case <-publishCtx.Done():
		return fmt.Errorf("%w: context timeout", ErrPublishTimeout)
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

	// 创建一个停止通道，用于通知处理goroutine退出
	stopCh := make(chan struct{})

	// 保存停止通道的引用，以便在取消订阅时关闭它
	// 注意：n.mu已经在Subscribe方法中被锁定，这里不需要再次锁定
	n.stopChans[topic] = stopCh

	// 启动goroutine处理接收到的消息
	go func() {
		defer close(outCh) // 确保在退出时关闭输出通道
		for {
			select {
			case <-stopCh:
				// 收到停止信号，退出goroutine
				return
			case msg, ok := <-msgCh:
				if !ok {
					// 消息通道已关闭，说明订阅已取消
					return
				}

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
		}
	}()

	return sub, nil
}

// subscribeJetStream 使用JetStream订阅
func (n *NatsBus) subscribeJetStream(topic string, outCh chan<- []byte) (*nats.Subscription, error) {
	// 创建唯一的消费者名称，避免在测试中冲突
	consumerName := fmt.Sprintf("%s-%s-%d", n.cfg.ConsumerName, topic, time.Now().UnixNano())

	// 使用流的唯一前缀构建完整主题
	fullTopic := topic
	if n.streamSubject != "" {
		fullTopic = n.streamSubject + "." + topic
	}

	// 创建或获取消费者，使用持久化消费者确保消息至少被处理一次
	subOpts := []nats.SubOpt{
		nats.AckExplicit(),             // 手动确认
		nats.DeliverAll(),              // 接收所有可用消息
		nats.MaxDeliver(5),             // 增加重新投递尝试次数
		nats.AckWait(60 * time.Second), // 增加确认等待时间
		nats.MaxAckPending(1000),       // 设置最大确认等待数
		nats.ManualAck(),               // 强制手动确认
	}

	slog.Info("creating jetstream subscription", "topic", fullTopic, "consumer", consumerName)

	// 使用PullSubscribe，让消费者控制获取速率
	sub, err := n.js.PullSubscribe(fullTopic, consumerName, subOpts...)
	if err != nil {
		slog.Error("failed to create jetstream subscription", "error", err, "topic", fullTopic)
		return nil, err
	}

	// 创建一个停止通道，用于通知处理goroutine退出
	stopCh := make(chan struct{})

	// 保存停止通道的引用，以便在取消订阅时关闭它
	// 注意：n.mu已经在Subscribe方法中被锁定，这里不需要再次锁定
	n.stopChans[topic] = stopCh

	// 启动goroutine处理接收到的消息
	go func() {
		defer close(outCh) // 确保在退出时关闭输出通道

		batchSize := 10             // 每次获取的消息数量
		waitTime := 1 * time.Second // 最长等待时间

		// 添加统计和恢复机制
		consecutiveErrors := 0
		maxConsecutiveErrors := 10
		backoffFactor := 1

		for {
			// 检查停止信号
			select {
			case <-stopCh:
				slog.Info("jetstream subscription worker stopping", "topic", topic)
				return
			default:
				// 继续处理
			}

			// 批量获取消息
			msgs, err := sub.Fetch(batchSize, nats.MaxWait(waitTime))
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					// 超时是正常的，检查是否有停止信号
					consecutiveErrors = 0 // 重置错误计数
					continue
				}

				// 针对严重错误立即退出
				if errors.Is(err, nats.ErrConnectionClosed) ||
					errors.Is(err, nats.ErrDrainTimeout) ||
					errors.Is(err, nats.ErrBadSubscription) {
					// 连接已关闭或订阅已失效，退出循环
					slog.Info("stopping jetstream subscription due to connection error",
						"error", err, "topic", topic)
					return
				}

				consecutiveErrors++
				n.IncSubscribeErrors()
				slog.Error("error fetching messages",
					"error", err,
					"topic", topic,
					"consecutive_errors", consecutiveErrors)

				// 如果连续错误太多，增加退避时间或考虑重置订阅
				if consecutiveErrors >= maxConsecutiveErrors {
					slog.Error("too many consecutive fetch errors, sleeping before retry",
						"topic", topic,
						"errors", consecutiveErrors)
					// 指数退避策略
					time.Sleep(time.Duration(backoffFactor) * 500 * time.Millisecond)
					if backoffFactor < 10 {
						backoffFactor *= 2
					}
				} else {
					// 常规错误暂停
					time.Sleep(200 * time.Millisecond)
				}

				continue
			}

			// 成功获取消息，重置错误计数和退避
			consecutiveErrors = 0
			backoffFactor = 1

			// 如果成功获取消息但数组为空，继续尝试
			if len(msgs) == 0 {
				continue
			}

			// 处理获取到的消息
			for _, msg := range msgs {
				// 检查停止信号
				select {
				case <-stopCh:
					// 尝试确认消息避免重新投递
					if err := msg.Ack(); err != nil {
						slog.Debug("failed to ack message during shutdown", "error", err)
					}
					slog.Info("jetstream message handler stopping", "topic", topic)
					return
				default:
					// 继续处理
				}

				// 复制消息数据
				data := make([]byte, len(msg.Data))
				copy(data, msg.Data)

				// 记录接收消息的元数据，用于调试
				meta, err := msg.Metadata()
				if err == nil && meta.NumDelivered > 1 {
					// 只记录重新投递的消息，减少日志量
					slog.Debug("received redelivered jetstream message",
						"topic", topic,
						"sequence", meta.Sequence.Stream,
						"timestamp", meta.Timestamp,
						"delivery_count", meta.NumDelivered)
				}

				// 发送到输出通道
				select {
				case outCh <- data:
					// 消息已成功发送到通道，确认消息
					if err := msg.Ack(); err != nil {
						slog.Error("failed to ack message", "error", err)
						n.IncAckErrors()
					}
				case <-time.After(300 * time.Millisecond): // 增加发送超时时间
					// 如果输出通道阻塞，记录警告并延迟重新投递
					slog.Warn("timeout sending message to subscriber channel", "topic", topic)
					n.IncSubscribeErrors()
					// 尝试稍后重新投递
					if err := msg.NakWithDelay(2 * time.Second); err != nil {
						slog.Error("failed to nak message", "error", err)
						n.IncAckErrors()
					}
				case <-stopCh:
					// 收到停止信号，退出
					// 尝试确认消息避免重新投递
					if err := msg.Ack(); err != nil {
						slog.Debug("failed to ack message during shutdown", "error", err)
					}
					return
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
		var err error

		// 先关闭停止通道，通知处理goroutine退出
		// 这应该在取消订阅之前完成，以避免goroutine继续处理已取消的订阅
		if stopCh, ok := n.stopChans[topic]; ok {
			close(stopCh)
			delete(n.stopChans, topic)
		}

		// 再取消订阅
		// 对于JetStream，我们需要使用Drain来确保所有消息都被处理
		if n.cfg.UseJetStream && n.js != nil {
			// 使用Drain而不是Unsubscribe，确保处理所有已接收的消息
			// Drain会等待所有正在处理的消息完成，然后取消订阅
			err = sub.Drain()

			// 给Drain一些时间完成
			time.Sleep(100 * time.Millisecond)
		} else {
			// 对于标准NATS，直接取消订阅
			err = sub.Unsubscribe()
		}

		// 从订阅映射中删除
		delete(n.subs, topic)

		// 如果取消订阅出错，返回错误
		if err != nil {
			slog.Error("failed to unsubscribe", "topic", topic, "error", err)
			return err
		}

		slog.Info("unsubscribed from topic", "topic", topic)
	}

	return nil
}

// SubscribeWithTimestamp 实现MessageBus.SubscribeWithTimestamp，订阅NATS主题并返回带时间戳的消息
func (n *NatsBus) SubscribeWithTimestamp(ctx context.Context, topic string) (<-chan *bus.Message, error) {
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
	outCh := make(chan *bus.Message, 100)

	// 根据配置选择订阅方式
	var sub *nats.Subscription
	var err error

	if n.cfg.UseJetStream && n.js != nil {
		// 使用JetStream订阅
		sub, err = n.subscribeJetStreamWithTimestamp(topic, outCh)
	} else {
		// 使用标准NATS订阅
		sub, err = n.subscribeStandardWithTimestamp(topic, outCh)
	}

	if err != nil {
		close(outCh)
		return nil, err
	}

	// 保存订阅对象，以便后续取消订阅
	n.subs[topic+"_timestamp"] = sub

	// 启动goroutine监控上下文取消
	go func() {
		<-ctx.Done()
		n.Unsubscribe(topic + "_timestamp")
	}()

	// 启动goroutine定期更新Pending消息数量（仅限JetStream）
	if n.cfg.UseJetStream && n.js != nil {
		go n.monitorPendingMessages(sub, topic)
	}

	slog.Info("subscribed to nats topic with timestamp", "topic", topic)
	return outCh, nil
}

// subscribeStandardWithTimestamp 使用标准NATS订阅并解析时间戳
func (n *NatsBus) subscribeStandardWithTimestamp(topic string, outCh chan<- *bus.Message) (*nats.Subscription, error) {
	// 使用通道订阅，NATS会自动将消息发送到msgCh
	msgCh := make(chan *nats.Msg, 100)
	sub, err := n.conn.ChanSubscribe(topic, msgCh)
	if err != nil {
		return nil, err
	}

	// 创建一个停止通道，用于通知处理goroutine退出
	stopCh := make(chan struct{})

	// 保存停止通道的引用，以便在取消订阅时关闭它
	n.stopChans[topic+"_timestamp"] = stopCh

	// 启动goroutine处理接收到的消息
	go func() {
		defer close(outCh) // 确保在退出时关闭输出通道
		for {
			select {
			case <-stopCh:
				// 收到停止信号，退出goroutine
				return
			case msg, ok := <-msgCh:
				if !ok {
					// 消息通道已关闭，说明订阅已取消
					return
				}

				// 解析带时间戳的消息
				message, err := bus.UnmarshalMessage(msg.Data)
				if err != nil {
					slog.Error("failed to unmarshal message with timestamp",
						"topic", topic, "error", err)
					n.IncSubscribeErrors()
					continue
				}

				// 计算并记录消息延迟
				latency := message.Latency()
				n.ObserveSubscribeLatency(latency)
				slog.Debug("message received with latency",
					"topic", topic,
					"latency_ms", latency.Milliseconds(),
					"timestamp", message.Timestamp)

				// 发送到输出通道
				select {
				case outCh <- message:
					// 消息已发送
				case <-time.After(n.cfg.OpTimeout):
					// 如果输出通道阻塞，记录警告
					slog.Warn("timeout sending message to subscriber channel", "topic", topic)
					n.IncSubscribeErrors()
				}
			}
		}
	}()

	return sub, nil
}

// subscribeJetStreamWithTimestamp 使用JetStream订阅并解析时间戳
func (n *NatsBus) subscribeJetStreamWithTimestamp(topic string, outCh chan<- *bus.Message) (*nats.Subscription, error) {
	// 创建唯一的消费者名称，避免在测试中冲突
	consumerName := fmt.Sprintf("%s-%s-ts-%d", n.cfg.ConsumerName, topic, time.Now().UnixNano())

	// 使用流的唯一前缀构建完整主题
	fullTopic := topic
	if n.streamSubject != "" {
		fullTopic = n.streamSubject + "." + topic
	}

	// 创建或获取消费者，使用持久化消费者确保消息至少被处理一次
	subOpts := []nats.SubOpt{
		nats.AckExplicit(),             // 手动确认
		nats.DeliverAll(),              // 接收所有可用消息
		nats.MaxDeliver(5),             // 增加重新投递尝试次数
		nats.AckWait(60 * time.Second), // 增加确认等待时间
		nats.MaxAckPending(1000),       // 设置最大确认等待数
		nats.ManualAck(),               // 强制手动确认
	}

	slog.Info("creating jetstream subscription with timestamp", "topic", fullTopic, "consumer", consumerName)

	// 使用PullSubscribe，让消费者控制获取速率
	sub, err := n.js.PullSubscribe(fullTopic, consumerName, subOpts...)
	if err != nil {
		slog.Error("failed to create jetstream subscription", "error", err, "topic", fullTopic)
		return nil, err
	}

	// 创建一个停止通道，用于通知处理goroutine退出
	stopCh := make(chan struct{})

	// 保存停止通道的引用，以便在取消订阅时关闭它
	n.stopChans[topic+"_timestamp"] = stopCh

	// 启动goroutine处理接收到的消息
	go func() {
		defer close(outCh) // 确保在退出时关闭输出通道

		batchSize := 10             // 每次获取的消息数量
		waitTime := 1 * time.Second // 最长等待时间

		// 添加统计和恢复机制
		consecutiveErrors := 0
		maxConsecutiveErrors := 10
		backoffFactor := 1

		for {
			// 检查停止信号
			select {
			case <-stopCh:
				slog.Info("jetstream subscription worker stopping", "topic", topic)
				return
			default:
				// 继续处理
			}

			// 批量获取消息
			msgs, err := sub.Fetch(batchSize, nats.MaxWait(waitTime))
			if err != nil {
				if errors.Is(err, nats.ErrTimeout) {
					// 超时是正常的，检查是否有停止信号
					consecutiveErrors = 0 // 重置错误计数
					continue
				}

				// 针对严重错误立即退出
				if errors.Is(err, nats.ErrConnectionClosed) ||
					errors.Is(err, nats.ErrDrainTimeout) ||
					errors.Is(err, nats.ErrBadSubscription) {
					// 连接已关闭或订阅已失效，退出循环
					slog.Info("stopping jetstream subscription due to connection error",
						"error", err, "topic", topic)
					return
				}

				consecutiveErrors++
				n.IncSubscribeErrors()
				slog.Error("error fetching messages",
					"error", err,
					"topic", topic,
					"consecutive_errors", consecutiveErrors)

				// 如果连续错误太多，增加退避时间或考虑重置订阅
				if consecutiveErrors >= maxConsecutiveErrors {
					slog.Error("too many consecutive fetch errors, sleeping before retry",
						"topic", topic,
						"errors", consecutiveErrors)
					// 指数退避策略
					time.Sleep(time.Duration(backoffFactor) * 500 * time.Millisecond)
					if backoffFactor < 10 {
						backoffFactor *= 2
					}
				} else {
					// 常规错误暂停
					time.Sleep(200 * time.Millisecond)
				}

				continue
			}

			// 成功获取消息，重置错误计数和退避
			consecutiveErrors = 0
			backoffFactor = 1

			// 如果成功获取消息但数组为空，继续尝试
			if len(msgs) == 0 {
				continue
			}

			// 处理获取到的消息
			for _, msg := range msgs {
				// 检查停止信号
				select {
				case <-stopCh:
					// 尝试确认消息避免重新投递
					if err := msg.Ack(); err != nil {
						slog.Debug("failed to ack message during shutdown", "error", err)
					}
					slog.Info("jetstream message handler stopping", "topic", topic)
					return
				default:
					// 继续处理
				}

				// 解析带时间戳的消息
				message, err := bus.UnmarshalMessage(msg.Data)
				if err != nil {
					slog.Error("failed to unmarshal message with timestamp",
						"topic", topic, "error", err)
					n.IncSubscribeErrors()
					// 确认消息以避免重新投递
					if err := msg.Ack(); err != nil {
						slog.Error("failed to ack invalid message", "error", err)
						n.IncAckErrors()
					}
					continue
				}

				// 计算并记录消息延迟
				latency := message.Latency()
				n.ObserveSubscribeLatency(latency)

				// 记录接收消息的元数据，用于调试
				meta, err := msg.Metadata()
				if err == nil && meta.NumDelivered > 1 {
					// 只记录重新投递的消息，减少日志量
					slog.Debug("received redelivered jetstream message with timestamp",
						"topic", topic,
						"sequence", meta.Sequence.Stream,
						"timestamp", meta.Timestamp,
						"delivery_count", meta.NumDelivered,
						"latency_ms", latency.Milliseconds())
				} else {
					slog.Debug("message received with latency",
						"topic", topic,
						"latency_ms", latency.Milliseconds(),
						"timestamp", message.Timestamp)
				}

				// 发送到输出通道
				select {
				case outCh <- message:
					// 消息已成功发送到通道，确认消息
					if err := msg.Ack(); err != nil {
						slog.Error("failed to ack message", "error", err)
						n.IncAckErrors()
					}
				case <-time.After(300 * time.Millisecond): // 增加发送超时时间
					// 如果输出通道阻塞，记录警告并延迟重新投递
					slog.Warn("timeout sending message to subscriber channel", "topic", topic)
					n.IncSubscribeErrors()
					// 尝试稍后重新投递
					if err := msg.NakWithDelay(2 * time.Second); err != nil {
						slog.Error("failed to nak message", "error", err)
						n.IncAckErrors()
					}
				case <-stopCh:
					// 收到停止信号，退出
					// 尝试确认消息避免重新投递
					if err := msg.Ack(); err != nil {
						slog.Debug("failed to ack message during shutdown", "error", err)
					}
					return
				}
			}
		}
	}()

	return sub, nil
}
