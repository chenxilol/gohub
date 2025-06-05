package hub

import (
	"encoding/json"
	"sync"
	"time"
)

// Message 表示一个通用的WebSocket消息
type Message struct {
	MessageID   int             `json:"message_id"`           // 消息ID，用于标识请求和响应
	MessageType string          `json:"message_type"`         // 消息类型，用于路由
	Data        json.RawMessage `json:"data"`                 // 消息数据，可以是任何JSON
	ClientID    string          `json:"client_id,omitempty"`  // 发送者客户端ID（可选）
	Timestamp   int64           `json:"timestamp"`            // 消息时间戳
	RequestID   int             `json:"request_id,omitempty"` // 请求ID，用于匹配响应
}

// NewMessage 创建一个新的消息
func NewMessage(messageID int, messageType string, data interface{}) (*Message, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return &Message{
		MessageID:   messageID,
		MessageType: messageType,
		Data:        dataBytes,
		Timestamp:   time.Now().UnixNano() / int64(time.Millisecond),
	}, nil
}

// Encode 将消息编码为JSON
func (m *Message) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// ReplyTo 创建一个对指定消息的回复
func (m *Message) ReplyTo(messageType string, data interface{}) (*Message, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	return &Message{
		MessageID:   m.MessageID,
		MessageType: messageType,
		Data:        dataBytes,
		ClientID:    m.ClientID,
		Timestamp:   time.Now().UnixNano() / int64(time.Millisecond),
		RequestID:   m.MessageID, // 使用原消息ID作为请求ID
	}, nil
}

// 消息解码

// DecodeMessage 将JSON数据解码为消息
func DecodeMessage(data []byte) (*Message, error) {
	var message Message
	if err := json.Unmarshal(data, &message); err != nil {
		return nil, err
	}
	return &message, nil
}

// AutoIncID 自动递增ID生成器
type AutoIncID struct {
	mu sync.Mutex
	id int
}

// NewAutoIncID 创建新的ID生成器
func NewAutoIncID() *AutoIncID {
	return &AutoIncID{id: 0}
}

// Next 获取下一个ID
func (g *AutoIncID) Next() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.id++
	return g.id
}
