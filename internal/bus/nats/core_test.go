package nats

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// 测试辅助函数，启动NATS服务器
func startNatsServer(t *testing.T) (string, func()) {
	// 检查是否安装了nats-server
	_, err := exec.LookPath("nats-server")
	if err != nil {
		t.Skip("nats-server not found in PATH, skipping test")
	}

	// 使用随机端口启动NATS服务器
	port, err := getRandomPort()
	if err != nil {
		t.Fatalf("Failed to get random port: %v", err)
	}

	url := fmt.Sprintf("nats://localhost:%d", port)
	cmd := exec.Command("nats-server", "-p", strconv.Itoa(port))

	// 将输出重定向到/dev/null
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start nats-server: %v", err)
	}

	// 等待服务器启动
	time.Sleep(500 * time.Millisecond)

	// 返回清理函数
	cleanup := func() {
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
			cmd.Wait()
		}
	}

	return url, cleanup
}

// 获取随机可用端口
func getRandomPort() (int, error) {
	// 使用端口0让系统分配一个可用端口
	// 这里我们简单地返回一个范围内的随机端口
	// 在实际测试中可能会有冲突风险
	return 10000 + int(time.Now().UnixNano()%10000), nil
}

// TestNatsBus_PublishSubscribe 测试基本的发布订阅功能
func TestNatsBus_PublishSubscribe(t *testing.T) {
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

	// 订阅主题
	topic := "test-topic"
	ch, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// 发布消息
	message := []byte("hello nats")
	if err := bus1.Publish(ctx, topic, message); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// 等待接收消息
	select {
	case received := <-ch:
		if string(received) != string(message) {
			t.Errorf("Expected message %q, got %q", message, received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

// TestNatsBus_MultipleSubscribers 测试多个订阅者
func TestNatsBus_MultipleSubscribers(t *testing.T) {
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

	// 创建多个订阅者
	topic := "test-topic-multi"
	ch1, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe ch1: %v", err)
	}

	ch2, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe ch2: %v", err)
	}

	// 发布消息
	message := []byte("hello multiple subscribers")
	if err := bus1.Publish(ctx, topic, message); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// 等待两个订阅者都收到消息
	for i, ch := range []<-chan []byte{ch1, ch2} {
		select {
		case received := <-ch:
			if string(received) != string(message) {
				t.Errorf("Subscriber %d: Expected message %q, got %q", i+1, message, received)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for message on subscriber %d", i+1)
		}
	}
}

// TestNatsBus_Unsubscribe 测试取消订阅
func TestNatsBus_Unsubscribe(t *testing.T) {
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

	// 订阅主题
	topic := "test-topic-unsub"
	ch, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// 发布第一条消息
	message1 := []byte("message before unsubscribe")
	if err := bus1.Publish(ctx, topic, message1); err != nil {
		t.Fatalf("Failed to publish first message: %v", err)
	}

	// 等待接收第一条消息
	select {
	case received := <-ch:
		if string(received) != string(message1) {
			t.Errorf("Expected message %q, got %q", message1, received)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first message")
	}

	// 取消订阅
	if err := bus1.Unsubscribe(topic); err != nil {
		t.Fatalf("Failed to unsubscribe: %v", err)
	}

	// 发布第二条消息
	message2 := []byte("message after unsubscribe")
	if err := bus1.Publish(ctx, topic, message2); err != nil {
		t.Fatalf("Failed to publish second message: %v", err)
	}

	// 确认不再接收消息（通道应该已关闭）
	select {
	case msg, ok := <-ch:
		if ok {
			t.Errorf("Unexpected message received after unsubscribe: %s", string(msg))
		}
		// 通道关闭是预期行为
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Expected channel to be closed after unsubscribe")
	}
}

// TestNatsBus_Reconnect 测试重连功能
func TestNatsBus_Reconnect(t *testing.T) {
	// 这个测试需要更复杂的设置，可能需要模拟服务器重启
	// 简化版：启动服务器，连接，关闭服务器，重启服务器，验证自动重连

	// 启动NATS服务器
	url, cleanup := startNatsServer(t)
	defer cleanup()

	// 创建NATS总线，配置快速重连
	cfg := DefaultConfig()
	cfg.URLs = []string{url}
	cfg.ReconnectWait = 100 * time.Millisecond

	bus1, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create NatsBus: %v", err)
	}
	defer bus1.Close()

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 订阅主题
	topic := "test-topic-reconnect"
	ch, err := bus1.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// 发布第一条消息
	message1 := []byte("message before disconnect")
	if err := bus1.Publish(ctx, topic, message1); err != nil {
		t.Fatalf("Failed to publish first message: %v", err)
	}

	// 等待接收第一条消息
	select {
	case received := <-ch:
		if string(received) != string(message1) {
			t.Errorf("Expected message %q, got %q", message1, received)
		}
	case <-time.After(2 * time.Second):
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
	time.Sleep(1 * time.Second)

	// 发布第二条消息
	message2 := []byte("message after reconnect")

	// 可能需要多次尝试，因为重连可能需要时间
	var publishErr error
	for i := 0; i < 5; i++ {
		publishErr = bus1.Publish(ctx, topic, message2)
		if publishErr == nil {
			break
		}
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
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for message after reconnect")
	}
}
