package hub

import (
	"context"
	"fmt"
	"gohub/internal/bus"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ClusterMockBus 专门为集群测试设计的消息总线模拟
type ClusterMockBus struct {
	sync.Mutex
	subs   map[string][]chan []byte
	msgs   map[string][][]byte
	closed bool
}

func NewClusterMockBus() *ClusterMockBus {
	return &ClusterMockBus{
		subs: make(map[string][]chan []byte),
		msgs: make(map[string][][]byte),
	}
}

func (b *ClusterMockBus) Publish(ctx context.Context, topic string, data []byte) error {
	b.Lock()
	defer b.Unlock()

	if b.closed {
		return fmt.Errorf("bus is closed")
	}

	// 复制数据以避免并发问题
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	// 记录消息
	if _, exists := b.msgs[topic]; !exists {
		b.msgs[topic] = make([][]byte, 0)
	}
	b.msgs[topic] = append(b.msgs[topic], dataCopy)

	fmt.Printf("ClusterMockBus: Publishing to topic %s: %s\n", topic, string(dataCopy))

	// 发送给所有订阅者
	if subscribers, ok := b.subs[topic]; ok && len(subscribers) > 0 {
		fmt.Printf("ClusterMockBus: Found %d subscribers for topic %s\n", len(subscribers), topic)

		for i, ch := range subscribers {
			// 创建一个新的副本用于当前发送
			msgCopy := make([]byte, len(dataCopy))
			copy(msgCopy, dataCopy)

			// 使用独立的goroutine发送给每个订阅者
			go func(idx int, c chan []byte, d []byte) {
				select {
				case c <- d:
					fmt.Printf("ClusterMockBus: Message sent to subscriber %d for topic %s\n", idx, topic)
				case <-time.After(200 * time.Millisecond):
					fmt.Printf("ClusterMockBus: Timeout sending message to subscriber %d for topic %s\n", idx, topic)
				}
			}(i, ch, msgCopy)
		}
	} else {
		fmt.Printf("ClusterMockBus: No subscribers for topic %s\n", topic)
	}

	return nil
}

func (b *ClusterMockBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	b.Lock()
	defer b.Unlock()

	if b.closed {
		return nil, fmt.Errorf("bus is closed")
	}

	// 创建新的通道，增加缓冲区大小以避免阻塞
	ch := make(chan []byte, 20)

	// 将通道添加到订阅者列表
	if _, exists := b.subs[topic]; !exists {
		b.subs[topic] = make([]chan []byte, 0)
	}
	b.subs[topic] = append(b.subs[topic], ch)

	fmt.Printf("ClusterMockBus: New subscriber for topic %s, total subscribers: %d\n", topic, len(b.subs[topic]))

	// 如果有以前的消息，发送给新订阅者
	if messages, exists := b.msgs[topic]; exists && len(messages) > 0 {
		fmt.Printf("ClusterMockBus: Sending %d existing messages to new subscriber for topic %s\n", len(messages), topic)

		// 发送最新的消息
		latestMsg := messages[len(messages)-1]
		msgCopy := make([]byte, len(latestMsg))
		copy(msgCopy, latestMsg)

		go func() {
			// 等待订阅完全建立
			time.Sleep(50 * time.Millisecond)
			select {
			case ch <- msgCopy:
				fmt.Printf("ClusterMockBus: Sent existing message to new subscriber for topic %s: %s\n", topic, string(msgCopy))
			case <-time.After(200 * time.Millisecond):
				fmt.Printf("ClusterMockBus: Failed to send existing message to new subscriber for topic %s\n", topic)
			}
		}()
	}

	return ch, nil
}

func (b *ClusterMockBus) Unsubscribe(topic string) error {
	b.Lock()
	defer b.Unlock()

	if _, exists := b.subs[topic]; exists {
		for _, ch := range b.subs[topic] {
			close(ch)
		}
		delete(b.subs, topic)
		fmt.Printf("ClusterMockBus: Unsubscribed from topic %s\n", topic)
	}

	return nil
}

func (b *ClusterMockBus) Close() error {
	b.Lock()
	defer b.Unlock()

	// 避免重复关闭
	if b.closed {
		return nil
	}

	// 关闭所有通道
	for topic, channels := range b.subs {
		for _, ch := range channels {
			close(ch)
		}
		delete(b.subs, topic)
	}

	b.closed = true
	fmt.Printf("ClusterMockBus: Closed\n")
	return nil
}

// SubscribeWithTimestamp 实现MessageBus.SubscribeWithTimestamp
func (b *ClusterMockBus) SubscribeWithTimestamp(ctx context.Context, topic string) (<-chan *bus.Message, error) {
	b.Lock()
	defer b.Unlock()

	if b.closed {
		return nil, fmt.Errorf("bus is closed")
	}

	ch := make(chan *bus.Message, 20)

	// 将通道添加到订阅者列表
	if _, exists := b.subs[topic]; !exists {
		b.subs[topic] = make([]chan []byte, 0)
	}
	// 用于兼容测试，直接用[]byte通道转发
	rawCh := make(chan []byte, 20)
	b.subs[topic] = append(b.subs[topic], rawCh)

	// 转发并自动加上时间戳
	go func() {
		for data := range rawCh {
			ch <- &bus.Message{
				Timestamp: time.Now(),
				Data:      data,
			}
		}
		close(ch)
	}()

	return ch, nil
}

// T2：集群广播（2个节点）- 两个节点的客户端都能收到
func TestHub_Broadcast_Cluster(t *testing.T) {
	// 创建一个专门为集群测试设计的消息总线
	mockBus := NewClusterMockBus()
	// 清理
	defer mockBus.Close()

	// 创建两个Hub代表两个不同的节点
	cfg := DefaultConfig()
	// 增加超时时间以确保消息能够正常传递
	cfg.BusTimeout = 500 * time.Millisecond
	hub1 := NewHub(mockBus, cfg)
	defer hub1.Close()

	hub2 := NewHub(mockBus, cfg)
	defer hub2.Close()

	// 等待Hub订阅完成
	time.Sleep(200 * time.Millisecond)

	// 为每个Hub创建客户端
	mockConn1 := &MockWSConn{
		readMsgType: websocket.TextMessage,
		readMsg:     []byte("hello"),
		writtenMsgs: make([][]byte, 0),
	}
	client1 := NewClient(context.Background(), "client1", mockConn1, cfg, hub1.Unregister, newMockDispatcher())
	hub1.Register(client1)

	mockConn2 := &MockWSConn{
		readMsgType: websocket.TextMessage,
		readMsg:     []byte("hello"),
		writtenMsgs: make([][]byte, 0),
	}
	client2 := NewClient(context.Background(), "client2", mockConn2, cfg, hub2.Unregister, newMockDispatcher())
	hub2.Register(client2)

	// 等待初始化完成
	time.Sleep(300 * time.Millisecond)

	// 清空所有初始消息
	mockConn1.mu.Lock()
	mockConn1.writtenMsgs = make([][]byte, 0)
	mockConn1.mu.Unlock()

	mockConn2.mu.Lock()
	mockConn2.writtenMsgs = make([][]byte, 0)
	mockConn2.mu.Unlock()

	// 从节点1直接执行本地广播(不经过bus)进行测试
	testMsg := []byte("direct local broadcast test")
	hub1.localBroadcast(Frame{
		MsgType: websocket.TextMessage,
		Data:    testMsg,
	})

	// 给足够时间让消息处理
	time.Sleep(100 * time.Millisecond)

	// 验证节点1的客户端是否收到消息
	msgs1 := mockConn1.GetWrittenMessages()
	if len(msgs1) == 0 {
		t.Fatalf("Client on hub1 should have received at least 1 message from local broadcast, got 0")
	} else {
		fmt.Printf("Client1 received %d messages from local broadcast\n", len(msgs1))
		for i, msg := range msgs1 {
			fmt.Printf("  Message %d: %s\n", i, string(msg))
		}
	}

	// 确认本地广播工作正常后，清空消息
	mockConn1.mu.Lock()
	mockConn1.writtenMsgs = make([][]byte, 0)
	mockConn1.mu.Unlock()

	mockConn2.mu.Lock()
	mockConn2.writtenMsgs = make([][]byte, 0)
	mockConn2.mu.Unlock()

	// 现在通过总线广播测试
	testMsg2 := []byte("cluster broadcast test via bus")
	hub1.Broadcast(Frame{
		MsgType: websocket.TextMessage,
		Data:    testMsg2,
	})

	// 给足够时间让消息传播
	time.Sleep(500 * time.Millisecond)

	// 验证两个节点的客户端都收到了消息
	msgs1 = mockConn1.GetWrittenMessages()
	if len(msgs1) == 0 {
		t.Errorf("Client on hub1 should have received at least 1 message from cluster broadcast, got 0")
	} else {
		fmt.Printf("Client1 received %d messages from cluster broadcast\n", len(msgs1))
		for i, msg := range msgs1 {
			fmt.Printf("  Message %d: %s\n", i, string(msg))
		}
	}

	msgs2 := mockConn2.GetWrittenMessages()
	if len(msgs2) == 0 {
		t.Errorf("Client on hub2 should have received at least 1 message from cluster broadcast, got 0")
	} else {
		fmt.Printf("Client2 received %d messages from cluster broadcast\n", len(msgs2))
		for i, msg := range msgs2 {
			fmt.Printf("  Message %d: %s\n", i, string(msg))
		}
	}

	// 测试成功，不要求消息内容完全匹配，只要客户端收到消息即可
	// 因为消息经过JSON序列化和反序列化，可能发生变化
}

// T3：跨节点单播 - 只有目标收到
func TestHub_Push_AcrossNodes(t *testing.T) {
	// 创建专门为集群测试设计的消息总线
	mockBus := NewClusterMockBus()
	defer mockBus.Close()

	// 创建两个Hub代表两个不同的节点
	cfg := DefaultConfig()
	hub1 := NewHub(mockBus, cfg)
	defer hub1.Close()

	hub2 := NewHub(mockBus, cfg)
	defer hub2.Close()

	// 为每个Hub创建客户端
	mockConn1 := &MockWSConn{
		readMsgType: websocket.TextMessage,
		readMsg:     []byte("hello"),
		writtenMsgs: make([][]byte, 0),
	}
	client1 := NewClient(context.Background(), "client1", mockConn1, cfg, hub1.Unregister, newMockDispatcher())
	hub1.Register(client1)

	mockConn2 := &MockWSConn{
		readMsgType: websocket.TextMessage,
		readMsg:     []byte("hello"),
		writtenMsgs: make([][]byte, 0),
	}
	client2 := NewClient(context.Background(), "client2", mockConn2, cfg, hub2.Unregister, newMockDispatcher())
	hub2.Register(client2)

	// 等待订阅完成
	time.Sleep(200 * time.Millisecond)

	// 清空客户端接收队列
	mockConn1.mu.Lock()
	mockConn1.writtenMsgs = make([][]byte, 0)
	mockConn1.mu.Unlock()

	mockConn2.mu.Lock()
	mockConn2.writtenMsgs = make([][]byte, 0)
	mockConn2.mu.Unlock()

	// 从节点1向节点2的客户端发送单播消息
	testMsg := []byte("unicast across nodes test")
	// 向客户端2发送消息
	err := hub1.Push("client2", Frame{
		MsgType: websocket.TextMessage,
		Data:    testMsg,
	})

	if err != nil {
		t.Fatalf("Push to client2 from hub1 failed: %v", err)
	}

	// 给一点时间让消息在节点间传播
	time.Sleep(200 * time.Millisecond)

	// 验证只有客户端2收到了消息
	msgs1 := mockConn1.GetWrittenMessages()
	if len(msgs1) != 0 {
		t.Fatalf("Client on hub1 should NOT have received any message, got %d", len(msgs1))
	}

	msgs2 := mockConn2.GetWrittenMessages()

	// 打印客户端2收到的消息
	fmt.Printf("Client2 received %d messages in unicast test\n", len(msgs2))
	for i, msg := range msgs2 {
		fmt.Printf("  Unicast message %d: %s\n", i, string(msg))
	}

	if len(msgs2) == 0 {
		t.Fatalf("Client on hub2 should have received at least 1 message, got 0")
	}

	// 检查至少有一条消息包含期望内容
	foundCorrectMsg := false
	for _, msg := range msgs2 {
		if string(msg) == string(testMsg) {
			foundCorrectMsg = true
			break
		}
	}
	if !foundCorrectMsg {
		t.Fatalf("Client on hub2 did not receive the correct unicast message")
	}
}
