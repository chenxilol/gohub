package redis

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"gohub/internal/bus"
)

// subscribeRoutine 订阅Redis频道
func (r *RedisBus) subscribeRoutine(ctx context.Context, topic string, outCh chan<- []byte) {
	var retryCount int32
	defer close(outCh)

	for {
		select {
		case <-ctx.Done():
			slog.Error("subscription context canceled, exiting", "topic", topic)
			return
		default:
		}
		pubsub := r.client.Subscribe(ctx, topic)

		func() {
			defer pubsub.Close()

			_, err := pubsub.Receive(ctx)
			if err != nil {
				slog.Error("failed to receive subscription confirmation", "topic", topic, "error", err)
				atomic.AddInt32(&retryCount, 1)
				time.Sleep(r.cfg.RetryInterval)
			}

			// 重置重试计数
			if retryCount > 0 {
				slog.Info("redis subscription recovered", "topic", topic, "after_retries", retryCount)
				atomic.StoreInt32(&retryCount, 0)
			}

			// 获取消息通道
			msgCh := pubsub.Channel()

			for msg := range msgCh {
				data := []byte(msg.Payload)

				select {
				case outCh <- data:
				case <-ctx.Done():
					return
				case <-time.After(r.cfg.OpTimeout):
					// 如果输出通道阻塞，记录警告
					slog.Warn("timeout sending message to subscriber channel",
						"topic", topic)
					r.IncSubscribeErrors()
				}
			}
		}()

		slog.Info("redis subscription disconnected, reconnecting",
			"topic", topic, "retry_count", atomic.AddInt32(&retryCount, 1))

		r.IncReconnects()
		time.Sleep(r.cfg.RetryInterval)
	}
}

// subscribeRoutineWithTimestamp 订阅Redis频道并解析时间戳
func (r *RedisBus) subscribeRoutineWithTimestamp(ctx context.Context, topic string, outCh chan<- *bus.Message) {
	var retryCount int32

	defer close(outCh)

	for {
		select {
		case <-ctx.Done():
			slog.Error("subscription context canceled, exiting", "topic", topic)
			return
		default:
		}
		pubsub := r.client.Subscribe(ctx, topic)

		// 确保在退出循环时关闭pubsub
		func() {
			defer pubsub.Close()

			_, err := pubsub.Receive(ctx)
			if err != nil {
				slog.Error("failed to receive subscription confirmation", "topic", topic, "error", err)
				atomic.AddInt32(&retryCount, 1)
				time.Sleep(r.cfg.RetryInterval)
			}

			if retryCount > 0 {
				slog.Info("redis subscription recovered", "topic", topic, "after_retries", retryCount)
				atomic.StoreInt32(&retryCount, 0)
			}

			msgCh := pubsub.Channel()
			for msg := range msgCh {
				message, err := bus.UnmarshalMessage([]byte(msg.Payload))
				if err != nil {
					slog.Error("failed to unmarshal message with timestamp", "topic", topic, "error", err)
					r.IncSubscribeErrors()
					continue
				}

				// 计算并记录消息延迟
				latency := message.Latency()
				r.ObserveSubscribeLatency(latency)
				slog.Debug("message received with latency", "topic", topic, "latency_ms", latency.Milliseconds(), "timestamp", message.Timestamp)

				select {
				case outCh <- message:
					// 消息已发送
				case <-ctx.Done():
					return
				case <-time.After(r.cfg.OpTimeout):
					// 如果输出通道阻塞，记录警告
					slog.Error("timeout sending message to subscriber channel", "topic", topic)
					r.IncSubscribeErrors()
				}
			}
		}()

		// 记录重连尝试
		slog.Info("redis subscription disconnected, reconnecting",
			"topic", topic, "retry_count", atomic.AddInt32(&retryCount, 1))

		r.IncReconnects()
		time.Sleep(r.cfg.RetryInterval)
	}
}
