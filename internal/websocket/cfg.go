// Package websocket 提供WebSocket连接抽象
package websocket

import "time"

// Config 定义WebSocket连接的配置选项
type Config struct {
	ReadTimeout      time.Duration // 读取超时时间
	WriteTimeout     time.Duration // 写入超时时间
	ReadBufferSize   int           // 读取缓冲区大小
	WriteBufferSize  int           // 写入缓冲区大小
	MessageBufferCap int           // 消息队列缓冲容量
}

// DefaultConfig 返回默认的WebSocket配置
func DefaultConfig() Config {
	return Config{
		ReadTimeout:      3 * time.Minute,
		WriteTimeout:     3 * time.Minute,
		ReadBufferSize:   4 << 10, // 4KB
		WriteBufferSize:  4 << 10, // 4KB
		MessageBufferCap: 256,     // 256条消息的队列
	}
}
