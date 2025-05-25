package websocket

import (
	"github.com/gorilla/websocket"
)

type GorillaConn struct {
	*websocket.Conn
}

var _ WSConn = (*GorillaConn)(nil)

func NewGorillaConn(conn *websocket.Conn) *GorillaConn {
	return &GorillaConn{Conn: conn}
}
