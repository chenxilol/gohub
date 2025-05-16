// Package nats 提供基于NATS的消息总线实现
package nats

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gohub/internal/bus"

	"github.com/nats-io/nats.go"
)

// Config NATS连接配置选项
type Config struct {
	// 连接地址，例如 nats://localhost:4222
	URLs []string

	// 连接名称，用于标识客户端
	Name string

	// 重连等待时间
	ReconnectWait time.Duration

	// 最大重连次数，-1表示无限重连
	MaxReconnects int

	// 连接超时
	ConnectTimeout time.Duration

	// 消息总线操作超时（发布超时）
	OpTimeout time.Duration

	// 是否使用JetStream
	UseJetStream bool

	// JetStream流名称
	StreamName string

	// JetStream消费者名称
	ConsumerName string

	// JetStream消息保留时间
	MessageRetention time.Duration
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		URLs:             []string{nats.DefaultURL},
		Name:             "gohub-client",
		ReconnectWait:    2 * time.Second,
		MaxReconnects:    -1, // 无限重连
		ConnectTimeout:   5 * time.Second,
		OpTimeout:        10 * time.Millisecond,
		UseJetStream:     false,
		StreamName:       "GOHUB",
		ConsumerName:     "gohub-consumer",
		MessageRetention: 1 * time.Hour,
	}
}

// NatsBus 基于NATS的消息总线实现
type NatsBus struct {
	conn       *nats.Conn
	js         nats.JetStreamContext
	cfg        Config
	mu         sync.RWMutex
	closed     bool
	subs       map[string]*nats.Subscription
	reconnects uint64 // 重连次数统计
}

// New 创建一个新的NatsBus实例
func New(cfg Config) (*NatsBus, error) {
	// 设置NATS连接选项
	opts := []nats.Option{
		nats.Name(cfg.Name),
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.Timeout(cfg.ConnectTimeout),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			slog.Warn("nats disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			slog.Info("nats reconnected")
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			slog.Info("nats connection closed")
		}),
	}

	// 连接到NATS服务器
	// 使用配置中的URLs，如果为空则使用默认URL
	serverURL := nats.DefaultURL
	if len(cfg.URLs) > 0 {
		// 对于多个URL，NATS客户端会自动尝试连接到其中任何一个
		serverURL = strings.Join(cfg.URLs, ",")
	}

	nc, err := nats.Connect(serverURL, opts...)
	if err != nil {
		return nil, err
	}

	nb := &NatsBus{
		conn: nc,
		cfg:  cfg,
		subs: make(map[string]*nats.Subscription),
	}

	// 如果配置了使用JetStream，初始化JetStream上下文
	if cfg.UseJetStream {
		if err := nb.setupJetStream(); err != nil {
			nc.Close()
			return nil, err
		}
	}

	slog.Info("connected to nats", "urls", cfg.URLs)
	return nb, nil
}

// setupJetStream 设置JetStream
func (n *NatsBus) setupJetStream() error {
	// 创建JetStream上下文
	js, err := n.conn.JetStream()
	if err != nil {
		return err
	}

	// 检查并创建流
	_, err = js.StreamInfo(n.cfg.StreamName)
	if err != nil {
		if errors.Is(err, nats.ErrStreamNotFound) {
			// 创建流
			_, err = js.AddStream(&nats.StreamConfig{
				Name:      n.cfg.StreamName,
				Subjects:  []string{">"},
				Retention: nats.WorkQueuePolicy,
				MaxAge:    n.cfg.MessageRetention,
			})
			if err != nil {
				return err
			}
			slog.Info("created jetstream stream", "name", n.cfg.StreamName)
		} else {
			return err
		}
	}

	n.js = js
	return nil
}

// GetReconnectCount 获取重连次数
func (n *NatsBus) GetReconnectCount() uint64 {
	return atomic.LoadUint64(&n.reconnects)
}

// Close 实现MessageBus.Close，关闭NATS连接
func (n *NatsBus) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.closed {
		return nil
	}

	// 标记为已关闭
	n.closed = true

	// 关闭所有订阅
	for _, sub := range n.subs {
		sub.Unsubscribe()
	}

	// 清空订阅映射
	n.subs = make(map[string]*nats.Subscription)

	// 关闭连接
	n.conn.Close()

	return nil
}

// 确保NatsBus实现了MessageBus接口
var _ bus.MessageBus = (*NatsBus)(nil)
