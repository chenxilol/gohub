package redis

import (
	"context"
	"testing"
	"time"

	"github.com/chenxilol/gohub/pkg/bus"

	"github.com/alicebob/miniredis/v2"
)

// setupTestRedis 创建一个miniredis服务器实例用于测试
func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *RedisBus) {
	// 创建miniredis服务器
	s, err := miniredis.Run()
	if err != nil {
		t.Fatalf("无法启动miniredis: %v", err)
	}

	// 创建Redis配置
	cfg := DefaultConfig()
	cfg.Addrs = []string{s.Addr()} // 使用miniredis地址
	cfg.OpTimeout = 50 * time.Millisecond

	// 创建RedisBus实例
	rb, err := New(cfg)
	if err != nil {
		t.Fatalf("无法创建RedisBus: %v", err)
	}

	return s, rb
}

// TestRedisBus_Connection 测试Redis连接
func TestRedisBus_Connection(t *testing.T) {
	// 创建miniredis和RedisBus
	s, rb := setupTestRedis(t)
	defer s.Close()
	defer rb.Close()

	// 验证连接已建立
	if rb.client == nil {
		t.Fatal("Redis客户端为nil")
	}

	// 测试连接有效性
	ctx := context.Background()
	if err := rb.client.Ping(ctx).Err(); err != nil {
		t.Fatalf("无法ping Redis: %v", err)
	}
}

// TestRedisBus_Publish 测试发布消息功能
func TestRedisBus_Publish(t *testing.T) {
	// 创建miniredis和RedisBus
	s, rb := setupTestRedis(t)
	defer s.Close()
	defer rb.Close()

	// 测试发布消息
	ctx := context.Background()
	topic := "test-topic"
	message := []byte("hello world")

	err := rb.Publish(ctx, topic, message)
	if err != nil {
		t.Fatalf("发布消息失败: %v", err)
	}

	// 验证发布的主题格式正确
	formattedTopic := rb.formatKey(topic)
	if formattedTopic != rb.cfg.KeyPrefix+topic {
		t.Errorf("主题格式不正确，期望 %s 但得到 %s", rb.cfg.KeyPrefix+topic, formattedTopic)
	}
}

// TestRedisBus_PublishTimeout 测试发布超时
func TestRedisBus_PublishTimeout(t *testing.T) {
	// 创建miniredis和RedisBus
	s, rb := setupTestRedis(t)

	// 设置非常短的超时
	rb.cfg.OpTimeout = 1 * time.Millisecond

	// 关闭Redis服务器，模拟连接问题
	s.Close()

	// 测试发布消息
	ctx := context.Background()
	err := rb.Publish(ctx, "test-topic", []byte("hello"))

	// 应该返回发布失败错误
	if err != bus.ErrPublishFailed {
		t.Errorf("期望错误 %v，但得到 %v", bus.ErrPublishFailed, err)
	}

	// 清理
	rb.Close()
}

// TestRedisBus_EmptyTopic 测试空主题
func TestRedisBus_EmptyTopic(t *testing.T) {
	// 创建miniredis和RedisBus
	s, rb := setupTestRedis(t)
	defer s.Close()
	defer rb.Close()

	// 测试发布空主题
	ctx := context.Background()
	err := rb.Publish(ctx, "", []byte("hello"))
	if err != bus.ErrTopicEmpty {
		t.Errorf("期望错误 %v，但得到 %v", bus.ErrTopicEmpty, err)
	}

	// 测试订阅空主题
	_, err = rb.Subscribe(ctx, "")
	if err != bus.ErrTopicEmpty {
		t.Errorf("期望错误 %v，但得到 %v", bus.ErrTopicEmpty, err)
	}

	// 测试取消订阅空主题
	err = rb.Unsubscribe("")
	if err != bus.ErrTopicEmpty {
		t.Errorf("期望错误 %v，但得到 %v", bus.ErrTopicEmpty, err)
	}
}

// TestRedisBus_Close 测试关闭功能
func TestRedisBus_Close(t *testing.T) {
	// 创建miniredis和RedisBus
	s, rb := setupTestRedis(t)
	defer s.Close()

	// 关闭RedisBus
	if err := rb.Close(); err != nil {
		t.Fatalf("关闭RedisBus失败: %v", err)
	}

	// 验证已标记为关闭
	if !rb.closed {
		t.Error("RedisBus未标记为关闭")
	}

	// 测试关闭后的操作
	ctx := context.Background()

	// 测试关闭后发布
	err := rb.Publish(ctx, "test-topic", []byte("hello"))
	if err != bus.ErrBusClosed {
		t.Errorf("期望错误 %v，但得到 %v", bus.ErrBusClosed, err)
	}

	// 测试关闭后订阅
	_, err = rb.Subscribe(ctx, "test-topic")
	if err != bus.ErrBusClosed {
		t.Errorf("期望错误 %v，但得到 %v", bus.ErrBusClosed, err)
	}

	// 测试重复关闭
	if err := rb.Close(); err != nil {
		t.Errorf("重复关闭应返回nil，但得到 %v", err)
	}
}
