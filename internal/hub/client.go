package hub

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// 定义错误
var (
	ErrClientClosed   = errors.New("client closed")
	ErrSendBufferFull = errors.New("send buffer full")
)

// Client 表示一个WebSocket客户端连接
type Client struct {
	id         string          // 客户端唯一标识
	conn       WSConn          // WebSocket连接
	out        chan Frame      // 发送消息队列
	ctx        context.Context // 上下文用于取消
	cancel     context.CancelFunc
	cfg        Config       // 配置选项
	onClose    func(string) // 关闭时的回调函数
	closed     sync.Once
	dispatcher MessageDispatcher // 添加 dispatcher 接口字段
}

// WSConn 解耦WebSocket连接接口，便于测试
type WSConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	WriteControl(int, []byte, time.Time) error
	SetReadLimit(int64)
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

// Config 客户端配置选项
type Config struct {
	ReadTimeout      time.Duration // 读取超时
	WriteTimeout     time.Duration // 写入超时
	ReadBufferSize   int           // 读取缓冲区大小
	WriteBufferSize  int           // 写入缓冲区大小
	MessageBufferCap int           // 消息队列容量
	BusTimeout       time.Duration // 消息总线操作超时
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		ReadTimeout:      3 * time.Minute,
		WriteTimeout:     3 * time.Minute,
		ReadBufferSize:   4 << 10,         // 4KB
		WriteBufferSize:  4 << 10,         // 4KB
		MessageBufferCap: 256,             // 256条消息缓冲
		BusTimeout:       5 * time.Second, // 消息总线超时
	}
}

// NewClient 创建一个新的客户端
func NewClient(ctx context.Context, id string, conn WSConn, cfg Config, onClose func(string), dispatcher MessageDispatcher) *Client {
	clientCtx, cancel := context.WithCancel(ctx)

	client := &Client{
		id:         id,
		conn:       conn,
		out:        make(chan Frame, cfg.MessageBufferCap),
		ctx:        clientCtx,
		cancel:     cancel,
		cfg:        cfg,
		onClose:    onClose,
		dispatcher: dispatcher,
	}

	// 启动读写循环
	go client.readLoop()
	go client.writeLoop()

	return client
}

// ID 获取客户端ID
func (c *Client) ID() string {
	return c.id
}

// Send 发送消息到客户端
func (c *Client) Send(f Frame) error {
	select {
	case c.out <- f:
		if f.Ack != nil {
			return <-f.Ack
		}
		return nil
	case <-c.ctx.Done():
		return ErrClientClosed
	default:
		return ErrSendBufferFull
	}
}

// readLoop 处理从客户端读取数据
func (c *Client) readLoop() {
	defer c.shutdown()

	c.conn.SetReadLimit(int64(c.cfg.ReadBufferSize))
	_ = c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))

	for {
		msgType, message, err := c.conn.ReadMessage()
		if err != nil {
			if IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure) {
				slog.Info("read error", "error", err, "client", c.id)
			}
			break
		}

		// 更新读取超时
		_ = c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))

		// 处理特殊的控制消息
		if msgType == websocket.PingMessage || msgType == websocket.PongMessage {
			continue
		}

		// 通常，JSON消息会以 TextMessage 类型发送
		// 如果是二进制消息，并且您的协议支持，也可以处理
		if msgType != websocket.TextMessage && msgType != websocket.BinaryMessage {
			slog.Warn("received unexpected message type for routing", "type", msgType, "client", c.id)
			continue
		}

		// 使用 dispatcher 处理消息
		if c.dispatcher == nil {
			slog.Error("dispatcher not initialized for client", "client", c.id)
			continue
		}
		if err := c.dispatcher.DecodeAndRoute(c.ctx, c, message); err != nil {
			slog.Error("failed to decode or route message", "error", err, "client", c.id)
			// 考虑根据错误类型决定是否关闭连接
			// 例如，如果错误是 json.Unmarshal 相关的，可能是无效的消息格式
		}
	}
}

// writeLoop 处理向客户端写入数据
func (c *Client) writeLoop() {
	defer c.shutdown()

	for {
		select {
		case <-c.ctx.Done():
			return

		case frame, ok := <-c.out:
			if !ok {
				// 通道关闭，发送关闭消息
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}

			_ = c.conn.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
			err := c.conn.WriteMessage(frame.MsgType, frame.Data)

			if frame.Ack != nil {
				frame.Ack <- err
			}

			if err != nil {
				slog.Info("write failed", "error", err, "client", c.id)
				return
			}
		}
	}
}

// shutdown 安全关闭客户端连接
func (c *Client) shutdown() {
	c.closed.Do(func() {
		c.cancel()

		// 在关闭连接前执行回调，通知Hub移除客户端
		if c.onClose != nil {
			c.onClose(c.id)
		}

		// 关闭连接
		_ = c.conn.Close()

		slog.Info("client disconnected", "id", c.id)
	})
}

// IsUnexpectedCloseError 判断是否为非预期的WebSocket关闭错误
func IsUnexpectedCloseError(err error, expectedCodes ...int) bool {
	if websocket.IsCloseError(err, expectedCodes...) {
		return false
	}
	if _, ok := err.(*websocket.CloseError); ok {
		return true
	}
	return true
}

// IsWebsocketCloseError 判断是否为正常的WebSocket关闭错误
func IsWebsocketCloseError(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived)
}
