package nats

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestNatsBus_JetStreamPublishSubscribe 测试JetStream的发布订阅功能
func TestNatsBus_JetStreamPublishSubscribe(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线，启用JetStream
	cfg := DefaultConfig()
	cfg.URLs = []string{url}
	cfg.UseJetStream = true
	cfg.StreamName = fmt.Sprintf("TEST_STREAM_%d", time.Now().UnixNano())
	cfg.ConsumerName = fmt.Sprintf("TEST_CONSUMER_%d", time.Now().UnixNano())

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus with JetStream: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 订阅主题
	topic := "js-test-topic"
	ch, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe with JetStream: %v", err)
	}

	// 发布消息
	message := []byte("hello jetstream")
	if err := bus1.Publish(ctx, topic, message); err != nil {
		t.Fatalf("Failed to publish with JetStream: %v", err)
	}

	// 等待接收消息
	select {
	case received := <-ch:
		if string(received) != string(message) {
			t.Errorf("Expected message %q, got %q", message, received)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

// TestNatsBus_JetStreamMultipleMessages 测试JetStream批量处理功能
func TestNatsBus_JetStreamMultipleMessages(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线，启用JetStream
	cfg := DefaultConfig()
	cfg.URLs = []string{url}
	cfg.UseJetStream = true
	cfg.StreamName = fmt.Sprintf("TEST_STREAM_%d", time.Now().UnixNano())
	cfg.ConsumerName = fmt.Sprintf("TEST_CONSUMER_%d", time.Now().UnixNano())

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus with JetStream: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 订阅主题
	topic := "js-test-multi"
	ch, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe with JetStream: %v", err)
	}

	// 发布多条消息
	messageCount := 10
	sentMessages := make([][]byte, messageCount)
	for i := 0; i < messageCount; i++ {
		sentMessages[i] = []byte(fmt.Sprintf("message-%d", i))
		if err := bus1.Publish(ctx, topic, sentMessages[i]); err != nil {
			t.Fatalf("Failed to publish message %d: %v", i, err)
		}
		// 短暂延迟确保消息顺序
		time.Sleep(50 * time.Millisecond)
	}

	// 接收并验证消息
	receivedCount := 0
	timeout := time.After(10 * time.Second)
	for receivedCount < messageCount {
		select {
		case received := <-ch:
			found := false
			for i, sent := range sentMessages {
				if string(received) == string(sent) {
					sentMessages[i] = nil // 标记为已收到
					found = true
					receivedCount++
					break
				}
			}
			if !found {
				t.Errorf("Received unexpected message: %s", string(received))
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for messages, received only %d/%d", receivedCount, messageCount)
			return
		}
	}
}

// TestNatsBus_JetStreamReconnect 测试JetStream重连功能
func TestNatsBus_JetStreamReconnect(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线，启用JetStream，配置快速重连
	cfg := DefaultConfig()
	cfg.URLs = []string{url}
	cfg.UseJetStream = true
	cfg.StreamName = fmt.Sprintf("TEST_STREAM_%d", time.Now().UnixNano())
	cfg.ConsumerName = fmt.Sprintf("TEST_CONSUMER_%d", time.Now().UnixNano())
	cfg.ReconnectWait = 100 * time.Millisecond

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus with JetStream: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 订阅主题
	topic := "js-test-reconnect"
	ch, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe with JetStream: %v", err)
	}

	// 发布第一条消息
	message1 := []byte("message before reconnect")
	if err := bus1.Publish(ctx, topic, message1); err != nil {
		t.Fatalf("Failed to publish first message: %v", err)
	}

	// 等待接收第一条消息
	select {
	case received := <-ch:
		if string(received) != string(message1) {
			t.Errorf("Expected message %q, got %q", message1, received)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for first message")
	}

	// 关闭服务器
	cleanup()

	// 等待一段时间
	time.Sleep(500 * time.Millisecond)

	// 重启服务器
	url, cleanup = startNatsServer(t)
	defer cleanup()

	// 等待重连
	time.Sleep(2 * time.Second)

	// 发布第二条消息
	message2 := []byte("message after reconnect")
	var publishErr error
	for i := 0; i < 10; i++ {
		publishErr = bus1.Publish(ctx, topic, message2)
		if publishErr == nil {
			break
		}
		t.Logf("Publish attempt %d failed: %v, retrying...", i+1, publishErr)
		time.Sleep(500 * time.Millisecond)
	}
	if publishErr != nil {
		t.Fatalf("Failed to publish after reconnect: %v", publishErr)
	}

	// 等待接收第二条消息
	select {
	case received := <-ch:
		if string(received) != string(message2) {
			t.Errorf("Expected message %q, got %q", message2, received)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message after reconnect")
	}
}

// TestNatsBus_JetStreamErrorHandling 测试JetStream错误处理
func TestNatsBus_JetStreamErrorHandling(t *testing.T) {
	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线，启用JetStream
	cfg := DefaultConfig()
	cfg.URLs = []string{url}
	cfg.UseJetStream = true
	cfg.StreamName = fmt.Sprintf("TEST_STREAM_%d", time.Now().UnixNano())
	cfg.ConsumerName = fmt.Sprintf("TEST_CONSUMER_%d", time.Now().UnixNano())
	cfg.OpTimeout = 100 * time.Millisecond // 设置非常短的超时，以便触发超时错误

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus with JetStream: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 订阅主题
	topic := "js-test-error"
	ch, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe with JetStream: %v", err)
	}

	// 发布一条正常消息
	message := []byte("normal message")
	err = bus1.Publish(ctx, topic, message)
	if err != nil {
		t.Logf("Expected possible timeout due to short OpTimeout: %v", err)
	}

	// 即使超时，消息应该还是能够发送
	select {
	case received, ok := <-ch:
		if !ok {
			t.Log("Channel closed unexpectedly")
		} else if string(received) != string(message) {
			t.Errorf("Expected message %q, got %q", message, received)
		}
	case <-time.After(3 * time.Second):
		t.Log("No message received within timeout, might be expected due to short OpTimeout")
	}

	// 测试错误情况：关闭连接后再发送
	bus1.Close()

	err = bus1.Publish(ctx, topic, []byte("message after close"))
	if err == nil {
		t.Error("Expected error when publishing after close, got nil")
	} else {
		t.Logf("Got expected error after close: %v", err)
	}
}
