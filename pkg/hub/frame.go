package hub

// Frame 封装底层websocket帧
type Frame struct {
	MsgType int        // websocket.TextMessage / BinaryMessage
	Data    []byte     // 消息内容
	Ack     chan error // nil表示发送后不需要确认,因消息是异步的，所以需要一个通道来确认消息是否发送成功
}
