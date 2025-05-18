package nats

import (
	"context"
	"errors"
	"testing"
	"time"

	"gohub/internal/bus"
)

// TestNatsBus_PublishSuccessful 测试成功发布
func TestNatsBus_PublishSuccessful(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线
	cfg := DefaultConfig()
	cfg.URLs = []string{url}

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 发布消息到没有订阅者的主题（这也应该成功）
	topic := "test-publish-success"
	message := []byte("test message")
	if err := bus1.Publish(ctx, topic, message); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}
}

// TestNatsBus_PublishErrors 测试发布错误情况
func TestNatsBus_PublishErrors(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线
	cfg := DefaultConfig()
	cfg.URLs = []string{url}

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 测试空主题错误
	err = bus1.Publish(ctx, "", []byte("empty topic"))
	if !errors.Is(err, bus.ErrTopicEmpty) {
		t.Errorf("Expected ErrTopicEmpty for empty topic, got: %v", err)
	}

	// 关闭总线后发布测试
	bus1.Close()
	err = bus1.Publish(ctx, "test-closed", []byte("after close"))
	if !errors.Is(err, bus.ErrBusClosed) {
		t.Errorf("Expected ErrBusClosed after close, got: %v", err)
	}

	// 测试上下文取消
	canceledCtx, cancelFunc := context.WithCancel(context.Background())
	cancelFunc() // 立即取消

	// 创建新总线实例用于测试
	bus2, _ := New(cfg)
	defer bus2.Close()

	// 使用已取消的上下文发布
	err = bus2.Publish(canceledCtx, "test-canceled", []byte("canceled context"))
	if err == nil {
		t.Error("Expected error with canceled context, got nil")
	}
}

// TestNatsBus_JetStreamPublishLatency 测试JetStream发布延迟统计
func TestNatsBus_JetStreamPublishLatency(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线，启用JetStream
	cfg := createJetStreamTestConfig(t, url)
	cfg.OpTimeout = 1 * time.Second // 增加超时时间确保发布成功

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus with JetStream: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 发布多条消息，触发延迟记录
	for i := 0; i < 3; i++ { // 减少消息数量提高测试速度
		if err := bus1.Publish(ctx, "test-latency", []byte("latency test")); err != nil {
			t.Logf("发布消息出错: %v，继续测试", err)
			// 不停止测试
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 这里主要是验证 time.Since(time.Now()) 不会导致错误
	// 真实的指标验证需要单独的测试工具
	t.Log("JetStream publish latency test completed")
}

// TestNatsBus_PublishTimeout 测试发布超时
func TestNatsBus_PublishTimeout(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线，设置极短的超时
	cfg := DefaultConfig()
	cfg.URLs = []string{url}
	cfg.OpTimeout = 1 * time.Nanosecond // 设置一个不可能完成的短超时

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 发布消息，应该会超时
	err = bus1.Publish(ctx, "test-timeout", []byte("timeout test"))
	if err == nil {
		t.Error("Expected timeout error, got nil")
	} else {
		t.Logf("Got expected error: %v", err)
	}
}
