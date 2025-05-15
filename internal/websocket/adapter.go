package websocket

import (
	"github.com/gorilla/websocket"
)

// GorillaConn 适配gorilla/websocket到WSConn接口
type GorillaConn struct {
	*websocket.Conn
}

// 确保GorillaConn实现了WSConn接口
var _ WSConn = (*GorillaConn)(nil)

// NewGorillaConn 创建一个新的gorilla适配器
func NewGorillaConn(conn *websocket.Conn) *GorillaConn {
	return &GorillaConn{Conn: conn}
}

// 为了便于测试和处理错误，添加一些实用函数
// FormatCloseMessage 格式化WebSocket关闭消息
func FormatCloseMessage(closeCode int, text string) []byte {
	return websocket.FormatCloseMessage(closeCode, text)
}

// IsCloseError 判断错误是否为特定的关闭错误码
func IsCloseError(err error, codes ...int) bool {
	return websocket.IsCloseError(err, codes...)
}

// 常量定义
const (
	// 消息类型
	TextMessage   = websocket.TextMessage
	BinaryMessage = websocket.BinaryMessage
	CloseMessage  = websocket.CloseMessage
	PingMessage   = websocket.PingMessage
	PongMessage   = websocket.PongMessage

	// 关闭码
	CloseNormalClosure    = websocket.CloseNormalClosure
	CloseGoingAway        = websocket.CloseGoingAway
	CloseNoStatusReceived = websocket.CloseNoStatusReceived
)
