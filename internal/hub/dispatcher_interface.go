package hub

import (
	"context"
	"encoding/json"
)

type MessageDispatcher interface {
	DecodeAndRoute(ctx context.Context, client *Client, data []byte) error // 消息解码并路由
	Register(msgType string, handler MessageHandlerFunc)                   // 注册消息处理函数
}

type MessageHandlerFunc func(ctx context.Context, client *Client, data json.RawMessage) error
