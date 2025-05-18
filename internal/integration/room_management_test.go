package integration

import (
	"context"
	"gohub/internal/bus/noop"
	"gohub/internal/hub"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestLocalRoomOperations 测试本地房间操作
func TestLocalRoomOperations(t *testing.T) {
	// 配置
	cfg := hub.DefaultConfig()

	// 创建Hub和NoOpBus
	noOpBus := noop.New()
	h := hub.NewHub(noOpBus, cfg)
	defer h.Close()

	// 模拟分发器
	dispatcher := newMockDispatcher()
	dispatcher.decodeAndRouteFunc = func(ctx context.Context, client *hub.Client, data []byte) error {
		// 在这里可以处理加入房间、离开房间、房间消息等
		return nil
	}

	// 创建房间
	roomID := "test_room_1"
	roomName := "测试房间1"
	maxClients := 100
	room, err := h.CreateRoom(roomID, roomName, maxClients)
	if err != nil {
		t.Fatalf("创建房间失败: %v", err)
	}

	if room.ID != roomID || room.Name != roomName || room.MaxClients != maxClients {
		t.Errorf("房间属性不匹配，期望ID=%s，Name=%s，MaxClients=%d", roomID, roomName, maxClients)
	}

	// 创建两个客户端
	mockConn1 := &MockWSConn{
		ReadMsgType: websocket.TextMessage,
		ReadMsg:     []byte(`{"message_type":"join_room", "room_id":"test_room_1"}`),
	}

	clientID1 := "client1"
	client1 := hub.NewClient(context.Background(), clientID1, mockConn1, cfg, h.Unregister, dispatcher)
	h.Register(client1)

	mockConn2 := &MockWSConn{
		ReadMsgType: websocket.TextMessage,
		ReadMsg:     []byte(`{"message_type":"join_room", "room_id":"test_room_1"}`),
	}

	clientID2 := "client2"
	client2 := hub.NewClient(context.Background(), clientID2, mockConn2, cfg, h.Unregister, dispatcher)
	h.Register(client2)

	// 等待客户端初始化
	time.Sleep(50 * time.Millisecond)

	// 1. 客户端1加入房间
	err = h.JoinRoom(roomID, clientID1)
	if err != nil {
		t.Fatalf("客户端1加入房间失败: %v", err)
	}

	// 验证客户端1是否在房间中
	room, err = h.GetRoom(roomID)
	if err != nil {
		t.Fatalf("获取房间失败: %v", err)
	}
	if !room.HasClient(clientID1) {
		t.Error("客户端1应该在房间中")
	}

	// 2. 客户端2加入房间
	err = h.JoinRoom(roomID, clientID2)
	if err != nil {
		t.Fatalf("客户端2加入房间失败: %v", err)
	}

	// 验证客户端2是否在房间中
	if !room.HasClient(clientID2) {
		t.Error("客户端2应该在房间中")
	}

	// 清空客户端消息
	mockConn1.ResetWrittenMessages()
	mockConn2.ResetWrittenMessages()

	// 3. 客户端1向房间发送消息
	roomMessage := []byte("hello room")
	err = h.BroadcastToRoom(roomID, hub.Frame{
		MsgType: websocket.TextMessage,
		Data:    roomMessage,
	}, "")

	if err != nil {
		t.Fatalf("向房间广播消息失败: %v", err)
	}

	// 等待消息处理
	time.Sleep(50 * time.Millisecond)

	// 验证两个客户端都收到了消息
	msgs1 := mockConn1.GetWrittenMessages()
	if len(msgs1) != 1 {
		t.Errorf("客户端1应该收到1条消息，实际收到 %d", len(msgs1))
	} else if string(msgs1[0]) != string(roomMessage) {
		t.Errorf("客户端1收到的消息内容不符，期望 %s，实际 %s", string(roomMessage), string(msgs1[0]))
	}

	msgs2 := mockConn2.GetWrittenMessages()
	if len(msgs2) != 1 {
		t.Errorf("客户端2应该收到1条消息，实际收到 %d", len(msgs2))
	} else if string(msgs2[0]) != string(roomMessage) {
		t.Errorf("客户端2收到的消息内容不符，期望 %s，实际 %s", string(roomMessage), string(msgs2[0]))
	}

	// 4. 客户端1离开房间
	err = h.LeaveRoom(roomID, clientID1)
	if err != nil {
		t.Fatalf("客户端1离开房间失败: %v", err)
	}

	// 验证客户端1是否已离开房间
	if room.HasClient(clientID1) {
		t.Error("客户端1应该已离开房间")
	}

	// 清空客户端消息
	mockConn1.ResetWrittenMessages()
	mockConn2.ResetWrittenMessages()

	// 5. 客户端2再次向房间发送消息
	roomMessage2 := []byte("hello again")
	err = h.BroadcastToRoom(roomID, hub.Frame{
		MsgType: websocket.TextMessage,
		Data:    roomMessage2,
	}, "")

	if err != nil {
		t.Fatalf("向房间广播消息失败: %v", err)
	}

	// 等待消息处理
	time.Sleep(50 * time.Millisecond)

	// 验证只有客户端2收到了消息
	msgs1 = mockConn1.GetWrittenMessages()
	if len(msgs1) != 0 {
		t.Errorf("客户端1应该收到0条消息，实际收到 %d", len(msgs1))
	}

	msgs2 = mockConn2.GetWrittenMessages()
	if len(msgs2) != 1 {
		t.Errorf("客户端2应该收到1条消息，实际收到 %d", len(msgs2))
	} else if string(msgs2[0]) != string(roomMessage2) {
		t.Errorf("客户端2收到的消息内容不符，期望 %s，实际 %s", string(roomMessage2), string(msgs2[0]))
	}

	// 6. 删除房间
	err = h.DeleteRoom(roomID)
	if err != nil {
		t.Fatalf("删除房间失败: %v", err)
	}

	// 验证房间是否已删除
	_, err = h.GetRoom(roomID)
	if err == nil {
		t.Error("房间应该已被删除")
	}
	if err != hub.ErrRoomNotFound {
		t.Errorf("应该返回ErrRoomNotFound错误，实际返回: %v", err)
	}
}
