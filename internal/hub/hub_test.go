package hub

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// MockWSConn 用于测试的WebSocket连接模拟
type MockWSConn struct {
	readMsg      []byte
	readMsgType  int
	readErr      error
	writtenMsgs  [][]byte
	writtenTypes []int
	writeErr     error
	closed       bool
	mu           sync.Mutex
}

func (m *MockWSConn) ReadMessage() (int, []byte, error) {
	return m.readMsgType, m.readMsg, m.readErr
}

func (m *MockWSConn) WriteMessage(msgType int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.writtenMsgs = append(m.writtenMsgs, data)
	m.writtenTypes = append(m.writtenTypes, msgType)
	return nil
}

func (m *MockWSConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return nil
}

func (m *MockWSConn) SetReadLimit(limit int64) {}

func (m *MockWSConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *MockWSConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (m *MockWSConn) Close() error {
	m.closed = true
	return nil
}

func (m *MockWSConn) GetWrittenMessages() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writtenMsgs
}

// MockWSConnBlocking 模拟写入被阻塞的WebSocket连接
type MockWSConnBlocking struct {
	MockWSConn
	writeBlocked chan struct{}
}

// 重写WriteMessage方法使其阻塞
func (m *MockWSConnBlocking) WriteMessage(msgType int, data []byte) error {
	// 阻塞写入，直到测试结束
	<-m.writeBlocked
	// 当解除阻塞后，调用原始实现
	return m.MockWSConn.WriteMessage(msgType, data)
}

// MockBus 用于测试的消息总线模拟
type MockBus struct {
	publishedTopics []string
	publishedData   [][]byte
	publishErr      error
	subs            map[string]chan []byte
	closed          bool
	mu              sync.Mutex
}

func NewMockBus() *MockBus {
	return &MockBus{
		publishedTopics: make([]string, 0),
		publishedData:   make([][]byte, 0),
		subs:            make(map[string]chan []byte),
	}
}

func (m *MockBus) Publish(ctx context.Context, topic string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishErr != nil {
		return m.publishErr
	}

	// 记录已发布的消息
	m.publishedTopics = append(m.publishedTopics, topic)
	m.publishedData = append(m.publishedData, data)

	// 检查是否有订阅者
	ch, ok := m.subs[topic]
	if !ok {
		fmt.Printf("MockBus: No subscribers for topic %s\n", topic)
		return nil
	}

	// 复制数据以避免并发问题
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	// 使用goroutine发送，避免在锁内阻塞
	go func() {
		select {
		case ch <- dataCopy:
			fmt.Printf("MockBus: Published message to topic %s: %s\n", topic, string(dataCopy))
		case <-time.After(100 * time.Millisecond):
			fmt.Printf("MockBus: Send timeout for topic %s\n", topic)
		}
	}()

	return nil
}

func (m *MockBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 创建一个足够大的通道
	ch := make(chan []byte, 20)
	m.subs[topic] = ch
	fmt.Printf("MockBus: Subscribed to topic %s\n", topic)

	// 如果是广播主题，且有之前的广播消息，发送给新订阅者
	if topic == BroadcastTopic {
		for i := len(m.publishedTopics) - 1; i >= 0; i-- {
			if m.publishedTopics[i] == BroadcastTopic {
				// 复制数据以避免并发问题
				dataCopy := make([]byte, len(m.publishedData[i]))
				copy(dataCopy, m.publishedData[i])

				// 使用goroutine发送，避免在锁内阻塞
				go func(data []byte) {
					// 等待订阅完全建立
					time.Sleep(10 * time.Millisecond)
					select {
					case ch <- data:
						fmt.Printf("MockBus: Sent existing broadcast: %s\n", string(data))
					case <-time.After(50 * time.Millisecond):
						fmt.Printf("MockBus: Failed to send existing broadcast\n")
					}
				}(dataCopy)

				// 只发送最近的一条
				break
			}
		}
	}

	return ch, nil
}

func (m *MockBus) Unsubscribe(topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.subs[topic]; ok {
		close(ch)
		delete(m.subs, topic)
	}
	return nil
}

func (m *MockBus) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for topic, ch := range m.subs {
		close(ch)
		delete(m.subs, topic)
	}
	return nil
}

// mockDispatcher 是 MessageDispatcher 的测试替身实现
type mockDispatcher struct {
	decodeAndRouteFunc func(ctx context.Context, client *Client, data []byte) error
}

// DecodeAndRoute 实现了 MessageDispatcher 接口
func (md *mockDispatcher) DecodeAndRoute(ctx context.Context, client *Client, data []byte) error {
	if md.decodeAndRouteFunc != nil {
		return md.decodeAndRouteFunc(ctx, client, data)
	}
	// 默认情况下，mock dispatcher 什么也不做，或者只记录一个debug日志（可选）
	// slog.Debug("mockDispatcher: DecodeAndRoute called", "client", client.ID())
	return nil
}

// newMockDispatcher 创建一个新的 mock dispatcher 实例
func newMockDispatcher() *mockDispatcher {
	return &mockDispatcher{}
}

// T1：单节点广播 - 所有本地客户端都能收到
func TestHub_Broadcast_SingleNode(t *testing.T) {
	// 创建Hub
	cfg := DefaultConfig()
	hub := NewHub(nil, cfg) // 不使用消息总线，模拟单节点模式

	// 创建多个客户端
	clientCount := 3
	mockConns := make([]*MockWSConn, clientCount)
	clients := make([]*Client, clientCount)

	for i := 0; i < clientCount; i++ {
		mockConns[i] = &MockWSConn{
			readMsgType: websocket.TextMessage,
			readMsg:     []byte("hello"),
			writtenMsgs: make([][]byte, 0), // 确保是空的
		}
		clients[i] = NewClient(context.Background(),
			fmt.Sprintf("client%d", i+1),
			mockConns[i],
			cfg,
			hub.Unregister,
			newMockDispatcher())
		hub.Register(clients[i])
	}

	// 确保客户端数量正确
	if count := hub.GetClientCount(); count != clientCount {
		t.Fatalf("Expected %d clients, got %d", clientCount, count)
	}

	// 广播消息
	testMsg := []byte("broadcast test message")
	hub.Broadcast(Frame{
		MsgType: websocket.TextMessage,
		Data:    testMsg,
	})

	// 给一点时间让消息被处理
	time.Sleep(50 * time.Millisecond)

	// 验证所有客户端都收到了消息
	for i, conn := range mockConns {
		msgs := conn.GetWrittenMessages()
		if len(msgs) != 1 {
			t.Fatalf("Client %d should have received 1 message, got %d", i, len(msgs))
		}
		if string(msgs[0]) != string(testMsg) {
			t.Fatalf("Client %d received wrong message: %s", i, string(msgs[0]))
		}
	}

	// 清理
	hub.Close()
}

// T4：心跳超时 - 客户端断开连接并被移除
func TestHub_ClientTimeout(t *testing.T) {
	// 创建Hub
	cfg := DefaultConfig()
	// 设置较短的超时时间便于测试
	cfg.ReadTimeout = 50 * time.Millisecond
	hub := NewHub(nil, cfg)

	// 创建一个客户端
	mockConn := &MockWSConn{
		readErr:     websocket.ErrReadLimit, // 模拟读取错误，触发断开
		writtenMsgs: make([][]byte, 0),      // 确保是空的
	}
	client := NewClient(context.Background(), "client1", mockConn, cfg, hub.Unregister, newMockDispatcher())
	hub.Register(client)

	// 确保客户端被注册
	// if count := hub.GetClientCount(); count != 1 { // 注释掉此断言，因为它可能在客户端因 readErr 立即失败后执行
	// 	t.Fatalf("Expected 1 client, got %d", count)
	// }

	// 等待readLoop因读取错误而退出
	time.Sleep(100 * time.Millisecond)

	// 验证客户端已被移除
	if count := hub.GetClientCount(); count != 0 {
		t.Fatalf("Expected 0 clients after timeout, got %d", count)
	}

	// 验证连接已关闭
	if !mockConn.closed {
		t.Fatal("Client connection should be closed")
	}

	// 清理
	hub.Close()
}

// T5：发送缓冲区已满 - 返回 ErrSendBufferFull
func TestHub_SendBufferFull(t *testing.T) {
	// 创建Hub
	cfg := DefaultConfig()
	// 设置很小的消息缓冲区容量
	cfg.MessageBufferCap = 1
	hub := NewHub(nil, cfg)

	// 创建一个会阻塞写入的客户端连接
	baseConn := &MockWSConn{
		readMsgType: websocket.TextMessage,
		readMsg:     []byte("hello"),
		writtenMsgs: make([][]byte, 0), // 确保是空的
	}

	mockConn := &MockWSConnBlocking{
		MockWSConn:   *baseConn,
		writeBlocked: make(chan struct{}),
	}

	clientID := "client1"
	client := NewClient(context.Background(), clientID, mockConn, cfg, hub.Unregister, newMockDispatcher())
	hub.Register(client)

	// 等待客户端goroutine启动
	time.Sleep(50 * time.Millisecond)

	// 首先发送一条消息，这条消息会阻塞在writeLoop中
	err := hub.Push(clientID, Frame{
		MsgType: websocket.TextMessage,
		Data:    []byte("first message to block writeLoop"),
	})

	if err != nil {
		t.Fatalf("First push should succeed: %v", err)
	}

	// 等待消息进入写循环
	time.Sleep(50 * time.Millisecond)

	// 发送足够多的消息以填满缓冲区
	// 由于writeLoop被阻塞，这些消息会堆积在out通道中
	testMsg := []byte("test message")
	for i := 0; i < cfg.MessageBufferCap; i++ {
		err := hub.Push(clientID, Frame{
			MsgType: websocket.TextMessage,
			Data:    testMsg,
		})
		if err != nil {
			t.Fatalf("Buffer filling message %d should succeed: %v", i, err)
		}
	}

	// 再发送一条消息，应该返回ErrSendBufferFull
	err = hub.Push(clientID, Frame{
		MsgType: websocket.TextMessage,
		Data:    testMsg,
	})

	if err != ErrSendBufferFull {
		t.Errorf("Expected ErrSendBufferFull, got %v", err)
	}

	// 解除WriteMessage的阻塞，让测试能够清理
	close(mockConn.writeBlocked)

	// 清理
	hub.Close()
}
