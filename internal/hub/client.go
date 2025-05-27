package hub

import (
	"context"
	"errors"
	"gohub/internal/auth"
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

type Client struct {
	id         string     // 客户端唯一标识
	conn       WSConn     // WebSocket连接
	out        chan Frame // 发送消息队列
	ctx        context.Context
	cancel     context.CancelFunc
	cfg        Config
	onClose    func(string) // 关闭时的回调函数
	closed     sync.Once
	dispatcher MessageDispatcher
	authClaims *auth.TokenClaims // 认证声明（如果已认证）
	metadata   sync.Map          // 客户端元数据，用于存储自定义信息
}

type WSConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	WriteControl(int, []byte, time.Time) error
	SetReadLimit(int64)
	SetReadDeadline(time.Time) error
	SetWriteDeadline(time.Time) error
	Close() error
}

type Config struct {
	ReadTimeout      time.Duration `mapstructure:"read_timeout" json:"read_timeout"`
	WriteTimeout     time.Duration `mapstructure:"write_timeout" json:"write_timeout"`
	ReadBufferSize   int           `mapstructure:"read_buffer_size" json:"read_buffer_size"`
	WriteBufferSize  int           `mapstructure:"write_buffer_size" json:"write_buffer_size"`
	MessageBufferCap int           `mapstructure:"message_buffer_cap" json:"message_buffer_cap"`
	BusTimeout       time.Duration `mapstructure:"bus_timeout" json:"bus_timeout"`
}

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

	go client.readLoop()
	go client.writeLoop()

	return client
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

func (c *Client) readLoop() {
	defer func() {
		c.shutdown()
	}()

	c.conn.SetReadLimit(int64(c.cfg.ReadBufferSize))
	_ = c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))

	for {
		msgType, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))

		if msgType == websocket.PingMessage || msgType == websocket.PongMessage {
			slog.Warn("received unexpected message type for routing", "type", msgType, "client", c.id)
			continue
		}

		if err := c.dispatcher.DecodeAndRoute(c.ctx, c, message); err != nil {
		}
	}
}

func (c *Client) writeLoop() {
	defer func() {
		c.shutdown()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return

		case frame, ok := <-c.out:
			if !ok {
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

func (c *Client) shutdown() {
	c.closed.Do(func() {
		c.cancel()
		if c.onClose != nil {
			c.onClose(c.id)
		}
		_ = c.conn.Close()
		slog.Info("client disconnected", "id", c.id)
	})
}

func (c *Client) Shutdown() {
	c.shutdown()
}

func IsUnexpectedCloseError(err error, expectedCodes ...int) bool {
	if websocket.IsCloseError(err, expectedCodes...) {
		return false
	}
	if _, ok := err.(*websocket.CloseError); ok {
		return true
	}
	return true
}

func IsWebsocketCloseError(err error) bool {
	return websocket.IsCloseError(err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived)
}

func (c *Client) ID() string {
	return c.id
}

// SetAuthClaims 设置客户端的认证声明
func (c *Client) SetAuthClaims(claims *auth.TokenClaims) {
	c.authClaims = claims
}

// GetAuthClaims 获取客户端的认证声明
func (c *Client) GetAuthClaims() *auth.TokenClaims {
	return c.authClaims
}

// IsAuthenticated 检查客户端是否已认证
func (c *Client) IsAuthenticated() bool {
	return c.authClaims != nil
}

// HasPermission 检查客户端是否具有特定权限
func (c *Client) HasPermission(permission auth.Permission) bool {
	if c.authClaims == nil {
		return false
	}

	return auth.HasPermission(c.authClaims, permission)
}

// SetMetadata 设置客户端元数据
func (c *Client) SetMetadata(key string, value interface{}) {
	c.metadata.Store(key, value)
}

// GetMetadata 获取客户端元数据
func (c *Client) GetMetadata(key string) (interface{}, bool) {
	return c.metadata.Load(key)
}

// DeleteMetadata 删除客户端元数据
func (c *Client) DeleteMetadata(key string) {
	c.metadata.Delete(key)
}
