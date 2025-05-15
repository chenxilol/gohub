// Package websocket 提供WebSocket连接抽象，使其易于测试和扩展
package websocket

import "time"

// WSConn 解耦Gorilla/nhooyr库，提高可测试性
type WSConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	WriteControl(int, []byte, time.Time) error
	SetReadLimit(int64)
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}
