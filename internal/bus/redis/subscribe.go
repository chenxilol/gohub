package redis

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"gohub/internal/bus"
)

// subscribeRoutine 在后台运行，处理来自Redis的订阅消息
// 该函数会在ctx取消时自动退出
func (r *RedisBus) subscribeRoutine(ctx context.Context, topic string, outCh chan<- []byte) {
	var retryCount int32

	defer close(outCh)

	for {
		select {
		case <-ctx.Done():
			slog.Info("subscription context canceled, exiting", "topic", topic)
			return
		default:
		}

		pubsub := r.client.Subscribe(ctx, topic)

		func() {
			defer pubsub.Close()

			_, err := pubsub.Receive(ctx)
			if err != nil {
				slog.Error("failed to receive subscription confirmation",
					"topic", topic, "error", err)

				atomic.AddInt32(&retryCount, 1)

				select {
				case <-ctx.Done():
					return
				case <-time.After(r.cfg.RetryInterval):
					return
				}
			}

			// 重置重试计数
			if retryCount > 0 {
				slog.Info("redis subscription recovered",
					"topic", topic, "after_retries", retryCount)
				atomic.StoreInt32(&retryCount, 0)
			}

			// 获取消息通道
			msgCh := pubsub.Channel()

			for msg := range msgCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				data := []byte(msg.Payload)
				fmt.Println(msg.Payload)

				// 注意：Redis PubSub 不提供消息时间戳，我们无法准确测量延迟
				// 如果需要延迟监控，建议在消息中包含时间戳信息

				// 发送消息到输出通道
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

		// 如果上下文已取消，退出循环
		select {
		case <-ctx.Done():
			return
		default:
			// 继续重试
		}

		// 记录重连尝试
		slog.Info("redis subscription disconnected, reconnecting",
			"topic", topic, "retry_count", atomic.AddInt32(&retryCount, 1))

		// 增加重连计数指标
		r.IncReconnects()

		// 等待重试间隔
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.cfg.RetryInterval):
			// 继续重试
		}
	}
}

// subscribeRoutineWithTimestamp 在后台运行，处理来自Redis的订阅消息并解析时间戳
// 该函数会在ctx取消时自动退出
func (r *RedisBus) subscribeRoutineWithTimestamp(ctx context.Context, topic string, outCh chan<- *bus.Message) {
	var retryCount int32

	defer close(outCh)

	for {
		select {
		case <-ctx.Done():
			slog.Info("subscription context canceled, exiting", "topic", topic)
			return
		default:
		}
		// 订阅Redis频道
		pubsub := r.client.Subscribe(ctx, topic)

		// 确保在退出循环时关闭pubsub
		func() {
			defer pubsub.Close()

			// 接收订阅确认消息
			_, err := pubsub.Receive(ctx)
			if err != nil {
				slog.Error("failed to receive subscription confirmation",
					"topic", topic, "error", err)

				// 增加重试计数
				atomic.AddInt32(&retryCount, 1)

				// 等待一段时间后重试
				select {
				case <-ctx.Done():
					return
				case <-time.After(r.cfg.RetryInterval):
					return
				}
			}

			// 重置重试计数
			if retryCount > 0 {
				slog.Info("redis subscription recovered",
					"topic", topic, "after_retries", retryCount)
				atomic.StoreInt32(&retryCount, 0)
			}

			// 获取消息通道
			msgCh := pubsub.Channel()

			// 处理接收到的消息
			for msg := range msgCh {
				// 检查上下文是否已取消
				select {
				case <-ctx.Done():
					return
				default:
					// 继续处理
				}

				// 解析带时间戳的消息
				message, err := bus.UnmarshalMessage([]byte(msg.Payload))
				if err != nil {
					slog.Error("failed to unmarshal message with timestamp",
						"topic", topic, "error", err)
					r.IncSubscribeErrors()
					continue
				}

				// 计算并记录消息延迟
				latency := message.Latency()
				r.ObserveSubscribeLatency(latency)
				slog.Debug("message received with latency",
					"topic", topic,
					"latency_ms", latency.Milliseconds(),
					"timestamp", message.Timestamp)

				// 发送消息到输出通道
				select {
				case outCh <- message:
					// 消息已发送
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

		// 如果上下文已取消，退出循环
		select {
		case <-ctx.Done():
			return
		default:
			// 继续重试
		}

		// 记录重连尝试
		slog.Info("redis subscription disconnected, reconnecting",
			"topic", topic, "retry_count", atomic.AddInt32(&retryCount, 1))

		// 增加重连计数指标
		r.IncReconnects()

		// 等待重试间隔
		select {
		case <-ctx.Done():
			return
		case <-time.After(r.cfg.RetryInterval):
			// 继续重试
		}
	}
}
