package bus

import (
	"context"
	"testing"
	"time"
)

// TestEtcdBus_Mock 使用模拟客户端测试EtcdBus
func TestEtcdBus_Mock(t *testing.T) {
	t.Run("Publish and Subscribe", func(t *testing.T) {
		// 跳过真实测试，除非明确启用
		t.Skip("Skipping etcd test - enable manually if etcd is available")

		// 创建配置
		cfg := DefaultEtcdConfig()

		// 创建EtcdBus实例
		bus, err := NewEtcdBus(cfg)
		if err != nil {
			t.Fatalf("Failed to create EtcdBus: %v", err)
		}
		defer bus.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// 订阅测试主题
		topic := "test-topic"
		msgCh, err := bus.Subscribe(ctx, topic)
		if err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// 发布消息
		testMsg := []byte("test message")
		err = bus.Publish(ctx, topic, testMsg)
		if err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// 等待接收消息
		select {
		case msg := <-msgCh:
			if string(msg) != string(testMsg) {
				t.Fatalf("Expected message %q, got %q", testMsg, msg)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for message")
		}

		// 测试取消订阅
		err = bus.Unsubscribe(topic)
		if err != nil {
			t.Fatalf("Failed to unsubscribe: %v", err)
		}
	})
}

// TestEtcdBus_ErrorHandling 测试错误处理
func TestEtcdBus_ErrorHandling(t *testing.T) {
	t.Run("Topic Empty", func(t *testing.T) {
		cfg := DefaultEtcdConfig()
		bus, err := NewEtcdBus(cfg)
		if err != nil {
			t.Skipf("Skipping test as etcd is not available: %v", err)
			return
		}
		defer bus.Close()

		// 测试空主题名
		err = bus.Publish(context.Background(), "", []byte("test"))
		if err != ErrTopicEmpty {
			t.Fatalf("Expected ErrTopicEmpty, got: %v", err)
		}

		_, err = bus.Subscribe(context.Background(), "")
		if err != ErrTopicEmpty {
			t.Fatalf("Expected ErrTopicEmpty for Subscribe, got: %v", err)
		}

		err = bus.Unsubscribe("")
		if err != ErrTopicEmpty {
			t.Fatalf("Expected ErrTopicEmpty for Unsubscribe, got: %v", err)
		}
	})
}

// TestEtcdBus_MultiSubscribe 测试多个订阅者
func TestEtcdBus_MultiSubscribe(t *testing.T) {
	t.Skip("Skipping etcd test - enable manually if etcd is available")

	cfg := DefaultEtcdConfig()
	bus, err := NewEtcdBus(cfg)
	if err != nil {
		t.Skipf("Skipping test as etcd is not available: %v", err)
		return
	}
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 创建两个订阅者
	topic := "multi-sub-test"
	ch1, err := bus.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe ch1: %v", err)
	}

	ch2, err := bus.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe ch2: %v", err)
	}

	// 发布消息
	testMsg := []byte("multi-subscriber test")
	err = bus.Publish(ctx, topic, testMsg)
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// 验证两个订阅者都收到了消息
	select {
	case msg := <-ch1:
		if string(msg) != string(testMsg) {
			t.Fatalf("Subscriber 1: Expected message %q, got %q", testMsg, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message on subscriber 1")
	}

	select {
	case msg := <-ch2:
		if string(msg) != string(testMsg) {
			t.Fatalf("Subscriber 2: Expected message %q, got %q", testMsg, msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message on subscriber 2")
	}
}
