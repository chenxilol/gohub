package bus

import (
	"context"
	"sync"
	"testing"
	"time"
)

// MockBus 实现MessageBus接口，用于测试
type MockBus struct {
	subs            map[string][]chan []byte
	publishedTopics []string
	publishedData   [][]byte
	mu              sync.Mutex
	closed          bool
}

// NewMockBus 创建一个新的模拟消息总线
func NewMockBus() *MockBus {
	return &MockBus{
		subs:            make(map[string][]chan []byte),
		publishedTopics: make([]string, 0),
		publishedData:   make([][]byte, 0),
		closed:          false,
	}
}

// Publish 实现MessageBus.Publish
func (m *MockBus) Publish(ctx context.Context, topic string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return ErrBusClosed
	}

	if topic == "" {
		return ErrTopicEmpty
	}

	m.publishedTopics = append(m.publishedTopics, topic)
	m.publishedData = append(m.publishedData, data)

	if channels, ok := m.subs[topic]; ok {
		dataCopy := make([]byte, len(data))
		copy(dataCopy, data)

		for _, ch := range channels {
			select {
			case ch <- dataCopy:
			default:
				// 通道已满，跳过
			}
		}
	}

	return nil
}

// Subscribe 实现MessageBus.Subscribe
func (m *MockBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrBusClosed
	}

	if topic == "" {
		return nil, ErrTopicEmpty
	}

	ch := make(chan []byte, 10)

	if _, ok := m.subs[topic]; !ok {
		m.subs[topic] = make([]chan []byte, 0)
	}

	m.subs[topic] = append(m.subs[topic], ch)
	return ch, nil
}

// Unsubscribe 实现MessageBus.Unsubscribe
func (m *MockBus) Unsubscribe(topic string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if topic == "" {
		return ErrTopicEmpty
	}

	if channels, ok := m.subs[topic]; ok {
		for _, ch := range channels {
			close(ch)
		}
		delete(m.subs, topic)
	}

	return nil
}

// Close 实现MessageBus.Close
func (m *MockBus) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true

	for topic, channels := range m.subs {
		for _, ch := range channels {
			close(ch)
		}
		delete(m.subs, topic)
	}

	return nil
}

// GetPublishedMessages 返回发布的消息，用于测试验证
func (m *MockBus) GetPublishedMessages() ([]string, [][]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	topics := make([]string, len(m.publishedTopics))
	data := make([][]byte, len(m.publishedData))

	copy(topics, m.publishedTopics)
	copy(data, m.publishedData)

	return topics, data
}

// TestMockBus 测试模拟消息总线
func TestMockBus(t *testing.T) {
	t.Run("Basic Publish and Subscribe", func(t *testing.T) {
		mockBus := NewMockBus()
		defer mockBus.Close()

		// 订阅主题
		ctx := context.Background()
		topic := "test-topic"
		ch, err := mockBus.Subscribe(ctx, topic)
		if err != nil {
			t.Fatalf("Failed to subscribe: %v", err)
		}

		// 发布消息
		testMsg := []byte("test message")
		err = mockBus.Publish(ctx, topic, testMsg)
		if err != nil {
			t.Fatalf("Failed to publish: %v", err)
		}

		// 接收消息
		select {
		case msg := <-ch:
			if string(msg) != string(testMsg) {
				t.Fatalf("Expected message %q, got %q", testMsg, msg)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for message")
		}

		// 验证发布的消息
		topics, data := mockBus.GetPublishedMessages()
		if len(topics) != 1 || topics[0] != topic {
			t.Fatalf("Expected topic %q, got %v", topic, topics)
		}
		if len(data) != 1 || string(data[0]) != string(testMsg) {
			t.Fatalf("Expected data %q, got %v", testMsg, data)
		}
	})

	t.Run("Multiple Subscribers", func(t *testing.T) {
		mockBus := NewMockBus()
		defer mockBus.Close()

		ctx := context.Background()
		topic := "multi-sub"

		// 两个订阅者
		ch1, _ := mockBus.Subscribe(ctx, topic)
		ch2, _ := mockBus.Subscribe(ctx, topic)

		// 发布消息
		testMsg := []byte("multi-subscriber test")
		_ = mockBus.Publish(ctx, topic, testMsg)

		// 验证两个订阅者都收到了消息
		select {
		case msg := <-ch1:
			if string(msg) != string(testMsg) {
				t.Fatalf("Sub1: Expected message %q, got %q", testMsg, msg)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for message on sub1")
		}

		select {
		case msg := <-ch2:
			if string(msg) != string(testMsg) {
				t.Fatalf("Sub2: Expected message %q, got %q", testMsg, msg)
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timeout waiting for message on sub2")
		}
	})

	t.Run("Error Conditions", func(t *testing.T) {
		mockBus := NewMockBus()

		// 测试空主题
		err := mockBus.Publish(context.Background(), "", []byte("test"))
		if err != ErrTopicEmpty {
			t.Fatalf("Expected ErrTopicEmpty, got %v", err)
		}

		_, err = mockBus.Subscribe(context.Background(), "")
		if err != ErrTopicEmpty {
			t.Fatalf("Expected ErrTopicEmpty, got %v", err)
		}

		// 关闭后尝试操作
		mockBus.Close()

		err = mockBus.Publish(context.Background(), "topic", []byte("test"))
		if err != ErrBusClosed {
			t.Fatalf("Expected ErrBusClosed, got %v", err)
		}

		_, err = mockBus.Subscribe(context.Background(), "topic")
		if err != ErrBusClosed {
			t.Fatalf("Expected ErrBusClosed, got %v", err)
		}
	})
}
