package bus

import (
	"encoding/json"
	"time"
)

type Message struct {
	Timestamp time.Time `json:"timestamp"`
	Data      []byte    `json:"data"`
}

func NewMessage(data []byte) *Message {
	return &Message{
		Timestamp: time.Now(),
		Data:      data,
	}
}

func (m *Message) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func UnmarshalMessage(data []byte) (*Message, error) {
	var msg Message
	err := json.Unmarshal(data, &msg)
	if err != nil {
		return nil, err
	}
	return &msg, nil
}

func (m *Message) Latency() time.Duration {
	return time.Since(m.Timestamp)
}
