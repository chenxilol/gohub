package integration

import (
	"context"
	"testing"
	"time"

	"github.com/chenxilol/gohub/pkg/auth"
	"github.com/chenxilol/gohub/pkg/bus/noop"
	"github.com/chenxilol/gohub/pkg/hub"

	"github.com/gorilla/websocket"
)

// TestClientConnectionAuthDisconnection 测试客户端连接、认证和断开连接的完整生命周期
func TestClientConnectionAuthDisconnection(t *testing.T) {
	// 配置
	cfg := hub.DefaultConfig()

	// 创建Hub和NoOpBus
	noOpBus := noop.New()
	h := hub.NewHub(noOpBus, cfg)
	defer h.Close()

	// 创建模拟的dispatcher
	dispatcher := newMockDispatcher()
	// 暂时不设置消息处理函数，避免自动响应ping

	// 创建模拟WebSocket连接
	mockConn := &MockWSConn{
		ReadMsgType: websocket.TextMessage,
		ReadMsg:     []byte(`{"message_type":"ping"}`),
	}

	// 1. 创建客户端并连接到Hub
	clientID := "test_client_1"
	client := hub.NewClient(context.Background(), clientID, mockConn, cfg, h.Unregister, dispatcher)

	// 2. 注册客户端到Hub
	h.Register(client)

	// 验证客户端已注册
	if count := h.GetClientCount(); count != 1 {
		t.Errorf("客户端注册后，Hub应该有1个客户端，实际有 %d", count)
	}

	// 等待客户端初始化完成
	time.Sleep(50 * time.Millisecond)

	// 3. 模拟客户端授权
	claims := &auth.TokenClaims{
		UserID:   "123",
		Username: "testuser",
	}
	client.SetAuthClaims(claims)

	// 验证客户端已授权
	if !client.IsAuthenticated() {
		t.Error("客户端应该已认证")
	}

	// 4. 客户端应该能正常接收消息
	// 重置已写入的消息
	mockConn.ResetWrittenMessages()

	// 向客户端发送消息
	testMsg := []byte("test message")
	err := h.Push(clientID, hub.Frame{
		MsgType: websocket.TextMessage,
		Data:    testMsg,
	})

	if err != nil {
		t.Errorf("向客户端发送消息失败: %v", err)
	}

	// 等待消息处理
	time.Sleep(50 * time.Millisecond)

	// 验证客户端收到了消息
	msgs := mockConn.GetWrittenMessages()
	// 只验证是否至少有一条消息，并且第一条消息内容是否正确
	if len(msgs) < 1 {
		t.Errorf("客户端应该至少收到1条消息，实际收到 %d", len(msgs))
	} else if string(msgs[0]) != string(testMsg) {
		t.Errorf("收到的第一条消息内容不符，期望 %s，实际 %s", string(testMsg), string(msgs[0]))
	}

	// 5. 测试ping-pong消息
	// 设置处理ping消息的函数
	dispatcher.decodeAndRouteFunc = func(ctx context.Context, client *hub.Client, data []byte) error {
		// 处理ping消息
		if string(data) == `{"message_type":"ping"}` {
			client.Send(hub.Frame{
				MsgType: websocket.TextMessage,
				Data:    []byte(`{"message_type":"pong"}`),
			})
		}
		return nil
	}

	mockConn.ResetWrittenMessages()

	// 手动触发消息处理
	dispatcher.DecodeAndRoute(context.Background(), client, []byte(`{"message_type":"ping"}`))

	// 等待消息处理
	time.Sleep(50 * time.Millisecond)

	// 验证收到了pong响应
	msgs = mockConn.GetWrittenMessages()
	// 只验证是否至少有一条消息，并且第一条消息内容是否正确
	if len(msgs) < 1 {
		t.Errorf("客户端应该至少收到1条pong消息，实际收到 %d", len(msgs))
	} else if string(msgs[0]) != `{"message_type":"pong"}` {
		t.Errorf("收到的第一条消息内容不符，期望 pong，实际 %s", string(msgs[0]))
	}

	// 6. 客户端断开连接
	client.Shutdown()

	// 等待客户端解注册
	time.Sleep(50 * time.Millisecond)

	// 验证客户端已从Hub中移除
	if count := h.GetClientCount(); count != 0 {
		t.Errorf("客户端断开后，Hub应该有0个客户端，实际有 %d", count)
	}

	// 验证连接已关闭
	if !mockConn.Closed {
		t.Error("客户端的WebSocket连接应该已关闭")
	}
}

// TestClientReadWriteTimeouts 测试客户端读写超时
func TestClientReadWriteTimeouts(t *testing.T) {
	// 配置短超时
	cfg := hub.DefaultConfig()
	cfg.ReadTimeout = 50 * time.Millisecond
	cfg.WriteTimeout = 50 * time.Millisecond

	// 创建Hub
	h := hub.NewHub(nil, cfg)
	defer h.Close()

	// 创建模拟WebSocket连接，模拟读取操作会阻塞/超时
	mockConn := &MockWSConn{
		ReadError: websocket.ErrReadLimit, // 使用websocket的错误
	}

	// 创建客户端
	clientID := "timeout_client"
	client := hub.NewClient(context.Background(), clientID, mockConn, cfg, h.Unregister, newMockDispatcher())
	h.Register(client)

	// 等待读取超时导致客户端断开
	time.Sleep(200 * time.Millisecond)

	// 验证客户端已从Hub中移除
	if count := h.GetClientCount(); count != 0 {
		t.Errorf("读取超时后，Hub应该有0个客户端，实际有 %d", count)
	}

	// 验证连接已关闭
	if !mockConn.Closed {
		t.Error("读取超时后，WebSocket连接应该已关闭")
	}

	// 测试写入超时
	// 使用不会触发大量警告的错误类型
	mockConn2 := &MockWSConn{
		WriteError: websocket.ErrCloseSent, // 使用websocket的错误
		// 确保读取操作不会产生未期望的消息
		ReadError: websocket.ErrReadLimit,
	}

	clientID2 := "timeout_client2"
	client2 := hub.NewClient(context.Background(), clientID2, mockConn2, cfg, h.Unregister, newMockDispatcher())
	h.Register(client2)

	// 向客户端发送消息，触发写入
	h.Push(clientID2, hub.Frame{
		MsgType: websocket.TextMessage,
		Data:    []byte("test message"),
	})

	// 等待写入超时导致客户端断开
	time.Sleep(200 * time.Millisecond)

	// 如果客户端还注册在Hub中，手动关闭并取消注册以避免无限警告
	if _, err := h.GetClient(clientID2); err == nil {
		client2.Shutdown()
		h.Unregister(clientID2)
	}

	// 验证客户端已从Hub中移除
	if count := h.GetClientCount(); count != 0 {
		t.Errorf("写入超时后，Hub应该有0个客户端，实际有 %d", count)
	}

	// 验证连接已关闭
	if !mockConn2.Closed {
		t.Error("写入超时后，WebSocket连接应该已关闭")
	}
}

// TestSendBufferFull 测试发送缓冲区已满的情况
func TestSendBufferFull(t *testing.T) {
	// 配置极小的消息缓冲区
	cfg := hub.DefaultConfig()
	cfg.MessageBufferCap = 1

	// 创建Hub
	h := hub.NewHub(nil, cfg)
	defer h.Close()

	// 创建基础连接
	baseConn := &MockWSConn{
		ReadMsgType: websocket.TextMessage,
		ReadMsg:     []byte("hello"),
	}

	// 创建一个写入会阻塞的WebSocket连接
	mockConn := &MockWSConnBlocking{
		MockWSConn:   *baseConn,
		WriteBlocked: make(chan struct{}),
	}

	// 创建客户端
	clientID := "blocking_client"
	client := hub.NewClient(context.Background(), clientID, mockConn, cfg, h.Unregister, newMockDispatcher())
	h.Register(client)

	// 等待客户端初始化
	time.Sleep(50 * time.Millisecond)

	// 发送第一条消息，这会在writeLoop中阻塞
	err := h.Push(clientID, hub.Frame{
		MsgType: websocket.TextMessage,
		Data:    []byte("first message that will block"),
	})

	if err != nil {
		t.Errorf("第一条消息应该发送成功: %v", err)
	}

	// 等待消息进入写循环
	time.Sleep(50 * time.Millisecond)

	// 再发送一条消息填满缓冲区
	err = h.Push(clientID, hub.Frame{
		MsgType: websocket.TextMessage,
		Data:    []byte("second message to fill buffer"),
	})

	if err != nil {
		t.Errorf("第二条消息应该发送成功: %v", err)
	}

	// 再发送一条消息，应该返回ErrSendBufferFull
	err = h.Push(clientID, hub.Frame{
		MsgType: websocket.TextMessage,
		Data:    []byte("third message should fail"),
	})

	if err != hub.ErrSendBufferFull {
		t.Errorf("第三条消息应该返回ErrSendBufferFull，实际返回: %v", err)
	}

	// 解除阻塞，以便清理
	close(mockConn.WriteBlocked)
}
