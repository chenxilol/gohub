package hub

import (
	"encoding/json"
	"time"
)

// Message 定义应用层消息结构
type Message struct {
	ID   int             `json:"message_id"`
	Type string          `json:"message_type"`
	Data json.RawMessage `json:"data,omitempty"`
	Ts   int64           `json:"ts,omitempty"`
}

// NewMessage 创建一个新的消息
func NewMessage(id int, msgType string, data interface{}) (*Message, error) {
	var rawData json.RawMessage
	var err error

	if data != nil {
		rawData, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	return &Message{
		ID:   id,
		Type: msgType,
		Data: rawData,
		Ts:   time.Now().UnixNano() / int64(time.Millisecond),
	}, nil
}

// Encode 将消息编码为JSON
func (m *Message) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// Decode 将消息的Data字段解码到指定结构
func (m *Message) Decode(v interface{}) error {
	return json.Unmarshal(m.Data, v)
}
