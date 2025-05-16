package noop

import (
	"context"
	"testing"
	"time"

	"gohub/internal/bus"
)

// TestNoopBus_PublishSubscribe 测试NoopBus的Publish和Subscribe功能
func TestNoopBus_PublishSubscribe(t *testing.T) {
	// 创建NoopBus实例
	noopBus := New()

	// 测试上下文
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// 测试基本的发布操作
	err := noopBus.Publish(ctx, "test-topic", []byte("hello"))
	if err != nil {
		t.Errorf("Expected nil error for Publish, got %v", err)
	}

	// 测试发布空主题
	err = noopBus.Publish(ctx, "", []byte("hello"))
	if err != bus.ErrTopicEmpty {
		t.Errorf("Expected ErrTopicEmpty for empty topic, got %v", err)
	}

	// 测试订阅
	ch, err := noopBus.Subscribe(ctx, "test-topic")
	if err != nil {
		t.Errorf("Expected nil error for Subscribe, got %v", err)
	}
	if ch == nil {
		t.Error("Expected non-nil channel from Subscribe")
	}

	// 测试订阅空主题
	_, err = noopBus.Subscribe(ctx, "")
	if err != bus.ErrTopicEmpty {
		t.Errorf("Expected ErrTopicEmpty for empty topic, got %v", err)
	}

	// 测试发布后通道不会收到消息
	select {
	case <-ch:
		t.Error("Did not expect to receive message on channel")
	case <-time.After(50 * time.Millisecond):
		// 预期行为: 通道不应该收到任何消息
	}

	// 测试取消订阅
	err = noopBus.Unsubscribe("test-topic")
	if err != nil {
		t.Errorf("Expected nil error for Unsubscribe, got %v", err)
	}

	// 测试取消订阅空主题
	err = noopBus.Unsubscribe("")
	if err != bus.ErrTopicEmpty {
		t.Errorf("Expected ErrTopicEmpty for empty topic, got %v", err)
	}

	// 测试关闭
	err = noopBus.Close()
	if err != nil {
		t.Errorf("Expected nil error for Close, got %v", err)
	}

	// 测试关闭后的操作
	err = noopBus.Publish(ctx, "test-topic", []byte("hello"))
	if err != bus.ErrBusClosed {
		t.Errorf("Expected ErrBusClosed for Publish after Close, got %v", err)
	}

	_, err = noopBus.Subscribe(ctx, "test-topic")
	if err != bus.ErrBusClosed {
		t.Errorf("Expected ErrBusClosed for Subscribe after Close, got %v", err)
	}

	// 测试重复关闭
	err = noopBus.Close()
	if err != nil {
		t.Errorf("Expected nil error for duplicate Close, got %v", err)
	}
}
