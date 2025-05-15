package hub

import "context"

// MessageDispatcher 定义了消息分发器需要实现的接口
// 这有助于解除 hub 包和具体 dispatcher 实现之间的循环依赖
type MessageDispatcher interface {
	// DecodeAndRoute 解码来自客户端的原始消息数据，并将其路由到适当的处理程序。
	// ctx: 请求的上下文，通常是客户端的上下文。
	// client: 发送消息的客户端实例。
	// data: 从客户端接收到的原始消息字节。
	// 返回值: 如果解码或路由过程中发生错误，则返回error。
	DecodeAndRoute(ctx context.Context, client *Client, data []byte) error
}
