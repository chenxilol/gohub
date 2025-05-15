// Package hub 提供WebSocket集线器核心功能
package hub

// Frame 封装底层websocket帧
type Frame struct {
	MsgType int        // websocket.TextMessage / BinaryMessage
	Data    []byte     // 消息内容
	Ack     chan error // nil表示发送后不需要确认
}
