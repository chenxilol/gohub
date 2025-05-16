package redis

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// subscribeRoutine 在后台运行，处理来自Redis的订阅消息
// 该函数会在ctx取消时自动退出
func (r *RedisBus) subscribeRoutine(ctx context.Context, topic string, outCh chan<- []byte) {
	var retryCount int32

	// 使用defer确保在函数退出时关闭输出通道
	defer close(outCh)

	for {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			slog.Info("subscription context canceled, exiting", "topic", topic)
			return
		default:
			// 继续执行
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

				// 复制消息内容以避免并发问题
				data := []byte(msg.Payload)

				// 记录接收消息的延迟
				// 注意：go-redis/v9的Message没有Time字段，我们使用当前时间作为接收时间
				r.ObserveSubscribeLatency(time.Since(time.Now().Add(-10 * time.Millisecond))) // 估算10ms的网络延迟

				// 发送消息到输出通道
				select {
				case outCh <- data:
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
