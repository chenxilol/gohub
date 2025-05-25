package integration

import (
	"context"
	"gohub/internal/hub"
	"sync"
	"time"
)

// MockWSConn 用于测试的WebSocket连接模拟
type MockWSConn struct {
	ReadMsg      []byte
	ReadMsgType  int
	ReadError    error
	writtenMsgs  [][]byte
	writtenTypes []int
	WriteError   error
	Closed       bool
	mu           sync.Mutex
}

func (m *MockWSConn) ReadMessage() (int, []byte, error) {
	return m.ReadMsgType, m.ReadMsg, m.ReadError
}

func (m *MockWSConn) WriteMessage(msgType int, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WriteError != nil {
		return m.WriteError
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
	m.Closed = true
	return nil
}

func (m *MockWSConn) GetWrittenMessages() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writtenMsgs
}

func (m *MockWSConn) ResetWrittenMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writtenMsgs = make([][]byte, 0)
	m.writtenTypes = make([]int, 0)
}

// MockWSConnBlocking 模拟写入被阻塞的WebSocket连接
type MockWSConnBlocking struct {
	MockWSConn
	WriteBlocked chan struct{}
}

// 重写WriteMessage方法使其阻塞
func (m *MockWSConnBlocking) WriteMessage(msgType int, data []byte) error {
	// 阻塞写入，直到测试结束
	<-m.WriteBlocked
	// 当解除阻塞后，调用原始实现
	return m.MockWSConn.WriteMessage(msgType, data)
}

// mockDispatcher 是 hub.MessageDispatcher 的模拟实现
type mockDispatcher struct {
	decodeAndRouteFunc func(ctx context.Context, client *hub.Client, data []byte) error
}

// DecodeAndRoute 实现了 MessageDispatcher 接口
func (md *mockDispatcher) DecodeAndRoute(ctx context.Context, client *hub.Client, data []byte) error {
	if md.decodeAndRouteFunc != nil {
		return md.decodeAndRouteFunc(ctx, client, data)
	}
	return nil
}

// newMockDispatcher 创建一个新的 mock dispatcher 实例
func newMockDispatcher() *mockDispatcher {
	return &mockDispatcher{}
}
func (md *mockDispatcher) Register(name string, handler hub.MessageHandlerFunc) {}
