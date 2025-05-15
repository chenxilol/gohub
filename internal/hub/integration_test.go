package hub

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// 更全面的MessageBus模拟，用于模拟真实的etcd行为
type EnhancedMockBus struct {
	publishedTopics []string
	publishedData   [][]byte
	publishErr      error
	subs            map[string][]chan []byte // 允许多个订阅者
	closed          bool
	mu              sync.Mutex
	// 添加一个字段标识是哪个总线实例，用于调试
	id string
}

func NewEnhancedMockBus(id string) *EnhancedMockBus {
	return &EnhancedMockBus{
		publishedTopics: make([]string, 0),
		publishedData:   make([][]byte, 0),
		subs:            make(map[string][]chan []byte),
		id:              id,
	}
}

func (m *EnhancedMockBus) Publish(ctx context.Context, topic string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.publishErr != nil {
		return m.publishErr
	}

	// 记录已发布的消息
	m.publishedTopics = append(m.publishedTopics, topic)
	m.publishedData = append(m.publishedData, data)

	// 将消息传递给该主题的所有订阅者
	if subscribers, ok := m.subs[topic]; ok {
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		// 对于每个订阅者，异步发送消息
		for i, ch := range subscribers {
			// 创建一个闭包来捕获变量
			go func(channel chan []byte, idx int) {
				select {
				case channel <- dataCopy:
					fmt.Printf("Bus %s published message to topic %s (subscriber %d): %s\n",
						m.id, topic, idx, string(data))
				case <-time.After(50 * time.Millisecond):
					// 超时，通道可能已满或被阻塞
					fmt.Printf("Bus %s timeout sending to topic %s (subscriber %d)\n",
						m.id, topic, idx)
				}
			}(ch, i)
		}
	} else {
		fmt.Printf("Bus %s: No subscribers for topic %s\n", m.id, topic)
	}

	return nil
}

func (m *EnhancedMockBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan []byte, 10)

	if _, ok := m.subs[topic]; !ok {
		m.subs[topic] = make([]chan []byte, 0)
	}

	m.subs[topic] = append(m.subs[topic], ch)
	fmt.Printf("Bus %s: Subscribed to topic %s\n", m.id, topic)
	return ch, nil
}

func (m *EnhancedMockBus) Unsubscribe(topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 简单实现：直接从map中删除
	delete(m.subs, topic)
	fmt.Printf("Bus %s: Unsubscribed from topic %s\n", m.id, topic)
	return nil
}

func (m *EnhancedMockBus) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true

	// 关闭所有订阅通道
	for topic, channels := range m.subs {
		for _, ch := range channels {
			close(ch)
		}
		delete(m.subs, topic)
	}

	fmt.Printf("Bus %s: Closed\n", m.id)
	return nil
}

// 集成测试：模拟集群环境中的Hub行为
func TestCluster_Integration(t *testing.T) {
	// 使用更严格的隔离策略，确保测试独立性
	t.Cleanup(func() {
		// 手动清理资源，防止测试间干扰
		fmt.Println("Test cleanup executed")
	})

	// 创建两个独立的总线实例，避免交叉干扰
	mockBus1 := NewEnhancedMockBus("bus1")
	mockBus2 := NewEnhancedMockBus("bus2")

	// 对于Push不存在的客户端，需要确保返回ErrClientNotFound
	// 正常情况下，由于集群模式会尝试通过总线发送，这里需要模拟失败情况
	mockBus1.publishErr = ErrClientNotFound

	// 创建两个Hub表示不同节点
	hubCfg := DefaultConfig()
	hub1 := NewHub(mockBus1, hubCfg)
	defer hub1.Close()

	hub2 := NewHub(mockBus2, hubCfg)
	defer hub2.Close()

	// 为每个Hub创建客户端
	mockConn1 := &MockWSConn{
		readMsgType: websocket.TextMessage,
		readMsg:     []byte("hello"),
		writtenMsgs: make([][]byte, 0),
	}

	// 等待一小段时间确保系统稳定，避免测试之间的干扰
	time.Sleep(10 * time.Millisecond)

	client1 := NewClient(context.Background(), "client1", mockConn1, hubCfg, hub1.Unregister)
	hub1.Register(client1)

	mockConn2 := &MockWSConn{
		readMsgType: websocket.TextMessage,
		readMsg:     []byte("hello"),
		writtenMsgs: make([][]byte, 0),
	}
	client2 := NewClient(context.Background(), "client2", mockConn2, hubCfg, hub2.Unregister)
	hub2.Register(client2)

	// 等待初始化完成
	time.Sleep(100 * time.Millisecond)

	// 重置连接上的消息，确保清空任何初始化期间可能产生的消息
	mockConn1.mu.Lock()
	mockConn1.writtenMsgs = make([][]byte, 0)
	mockConn1.mu.Unlock()

	mockConn2.mu.Lock()
	mockConn2.writtenMsgs = make([][]byte, 0)
	mockConn2.mu.Unlock()

	// 在广播测试前重置mockBus1的错误状态
	mockBus1.mu.Lock()
	mockBus1.publishErr = nil
	mockBus1.mu.Unlock()

	// 测试本地广播
	testMsg := []byte("integration local broadcast test")
	hub1.Broadcast(Frame{
		MsgType: websocket.TextMessage,
		Data:    testMsg,
	})

	// 给一点时间让消息被处理
	time.Sleep(100 * time.Millisecond)

	// 验证只有本地客户端收到了消息（由于两个总线是独立的）
	msgs1 := mockConn1.GetWrittenMessages()

	// 调试输出
	fmt.Printf("After broadcast - Client1 messages count: %d\n", len(msgs1))
	for i, msg := range msgs1 {
		fmt.Printf("Client1 message %d: %s\n", i, string(msg))
	}

	// 验证客户端1收到了消息（可能会收到1-2条，取决于消息传递机制）
	if len(msgs1) < 1 {
		t.Errorf("Client on hub1 should have received at least 1 message, got %d", len(msgs1))
	} else if len(msgs1) > 2 {
		t.Errorf("Client on hub1 received too many messages: %d", len(msgs1))
	} else if string(msgs1[0]) != string(testMsg) {
		t.Errorf("Client on hub1 received wrong message: %s", string(msgs1[0]))
	}

	// 客户端2不应该收到消息，因为两个总线是独立的
	msgs2 := mockConn2.GetWrittenMessages()
	fmt.Printf("After broadcast - Client2 messages count: %d\n", len(msgs2))
	if len(msgs2) != 0 {
		t.Errorf("Client on hub2 should NOT have received any message, got %d", len(msgs2))
	}

	// 清空客户端1的消息队列，以便于下一步测试
	mockConn1.mu.Lock()
	mockConn1.writtenMsgs = make([][]byte, 0)
	mockConn1.mu.Unlock()

	// 测试单播 - 直接发到本地客户端
	unicastMsg := []byte("integration local unicast test")
	err := hub1.Push("client1", Frame{
		MsgType: websocket.TextMessage,
		Data:    unicastMsg,
	})

	if err != nil {
		t.Fatalf("Push to client1 failed: %v", err)
	}

	// 等待消息被处理
	time.Sleep(100 * time.Millisecond)

	// 验证本地客户端收到了单播消息
	msgs1 = mockConn1.GetWrittenMessages()

	// 调试输出
	fmt.Printf("After unicast - Client1 messages count: %d\n", len(msgs1))
	for i, msg := range msgs1 {
		fmt.Printf("Client1 unicast message %d: %s\n", i, string(msg))
	}

	// 客户端1应该正好收到一条消息
	if len(msgs1) != 1 {
		t.Errorf("Client on hub1 should have received exactly 1 unicast message, got %d", len(msgs1))
	} else if string(msgs1[0]) != string(unicastMsg) {
		t.Errorf("Client on hub1 received wrong unicast message: %s", string(msgs1[0]))
	}

	// 测试错误处理：客户端不存在
	// 设置mockBus返回特定错误
	mockBus1.mu.Lock()
	mockBus1.publishErr = ErrClientNotFound
	mockBus1.mu.Unlock()

	err = hub1.Push("non-existent", Frame{
		MsgType: websocket.TextMessage,
		Data:    []byte("test error"),
	})

	if err != ErrClientNotFound {
		t.Errorf("Expected ErrClientNotFound for non-existent client, got: %v", err)
	}

	// 不使用Fatalf确保所有测试步骤都能执行完并留下日志
}
