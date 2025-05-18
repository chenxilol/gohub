// Package nats 提供基于NATS的消息总线实现
package nats

import (
	"errors"
	"fmt"
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
	URLs             []string      `mapstructure:"urls" json:"urls"` //  连接地址
	Name             string        `mapstructure:"name" json:"name"`
	ReconnectWait    time.Duration `mapstructure:"reconnect_wait" json:"reconnect_wait"`
	MaxReconnects    int           `mapstructure:"max_reconnects" json:"max_reconnects"`
	ConnectTimeout   time.Duration `mapstructure:"connect_timeout" json:"connect_timeout"`
	OpTimeout        time.Duration `mapstructure:"op_timeout" json:"op_timeout"`       //  所有操作的超时时间，包括连接、订阅、发布、消费等
	UseJetStream     bool          `mapstructure:"use_jetstream" json:"use_jetstream"` // 是否使用JetStream，如果启用，则使用JetStream来实现消息持久化
	StreamName       string        `mapstructure:"stream_name" json:"stream_name"`
	ConsumerName     string
	MessageRetention time.Duration `mapstructure:"message_retention" json:"message_retention"` // 消息保留时间
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
	conn          *nats.Conn
	js            nats.JetStreamContext
	cfg           Config
	mu            sync.RWMutex
	closed        bool
	subs          map[string]*nats.Subscription
	stopChans     map[string]chan struct{} // 用于通知订阅goroutine退出的通道
	reconnects    uint64                   // 重连次数统计
	streamSubject string                   // JetStream流的唯一主题前缀
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
		nats.ReconnectHandler(func(conn *nats.Conn) {
			// 记录服务器URL，便于调试
			serverURL := ""
			if conn.ConnectedUrl() != "" {
				serverURL = conn.ConnectedUrl()
			}

			slog.Info("nats reconnected", "server", serverURL, "attempts", conn.Reconnects)
		}),
		nats.ClosedHandler(func(conn *nats.Conn) {
			if conn.LastError() != nil {
				slog.Error("nats connection closed with error", "error", conn.LastError())
			} else {
				slog.Info("nats connection closed gracefully")
			}
		}),
		nats.ErrorHandler(func(conn *nats.Conn, sub *nats.Subscription, err error) {
			if sub != nil {
				delivered, _ := sub.Delivered()
				slog.Error("nats async error",
					"error", err,
					"subject", sub.Subject,
					"queue", sub.Queue,
					"delivered", delivered)
			} else {
				slog.Error("nats connection error", "error", err)
			}
		}),
		// 添加重试逻辑，减少连接失败的可能性
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(cfg.MaxReconnects),
		// 设置Ping间隔以检测连接状态
		nats.PingInterval(20 * time.Second),
		nats.MaxPingsOutstanding(5),
	}

	// 连接到NATS服务器
	// 使用配置中的URLs，如果为空则使用默认URL
	serverURL := nats.DefaultURL
	if len(cfg.URLs) > 0 {
		// 对于多个URL，NATS客户端会自动尝试连接到其中任何一个
		serverURL = strings.Join(cfg.URLs, ",")
	}

	slog.Info("connecting to nats", "urls", cfg.URLs)
	nc, err := nats.Connect(serverURL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// 创建并初始化NatsBus实例
	nb := &NatsBus{
		conn:      nc,
		cfg:       cfg,
		subs:      make(map[string]*nats.Subscription),
		stopChans: make(map[string]chan struct{}),
	}

	// 如果配置了使用JetStream，初始化JetStream上下文
	if cfg.UseJetStream {
		if err := nb.setupJetStream(); err != nil {
			// 关闭连接并返回错误
			nc.Close()
			return nil, fmt.Errorf("failed to setup JetStream: %w", err)
		}
	}

	connectedUrl := nc.ConnectedUrl()
	if connectedUrl == "" {
		connectedUrl = "unknown"
	}
	slog.Info("connected to nats",
		"server", connectedUrl,
		"jetstream_enabled", cfg.UseJetStream)
	return nb, nil
}

// setupJetStream 设置JetStream
func (n *NatsBus) setupJetStream() error {
	// 创建JetStream上下文
	js, err := n.conn.JetStream(nats.MaxWait(5 * time.Second))
	if err != nil {
		return fmt.Errorf("failed to create JetStream context: %w", err)
	}

	// 使用固定的主题前缀，而不是每次启动生成新的
	// 这确保了服务重启后的消息连续性
	streamSubject := n.cfg.StreamName

	// 添加服务名称信息以支持多个服务使用同一个NATS服务器
	if n.cfg.Name != "" {
		streamSubject = streamSubject + "." + n.cfg.Name
	}

	// 保存流主题前缀，用于后续发布
	n.streamSubject = streamSubject

	slog.Info("creating/checking jetstream stream", "name", n.cfg.StreamName, "subject", streamSubject)

	// 检查并创建流
	var streamInfo *nats.StreamInfo

	// 添加重试逻辑
	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		streamInfo, err = js.StreamInfo(n.cfg.StreamName)
		if err == nil {
			// 流已存在
			break
		}

		if !errors.Is(err, nats.ErrStreamNotFound) {
			// 其他错误，继续重试
			lastErr = err
			slog.Warn("error checking stream info, retrying",
				"attempt", i+1,
				"error", err,
				"stream", n.cfg.StreamName)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		// 流不存在，创建流
		streamConfig := &nats.StreamConfig{
			Name:         n.cfg.StreamName,
			Subjects:     []string{streamSubject + ".>"}, // 使用固定前缀捕获主题
			Retention:    nats.WorkQueuePolicy,
			MaxAge:       n.cfg.MessageRetention,
			NoAck:        false,              // 默认需要确认
			Discard:      nats.DiscardOld,    // 丢弃旧消息
			Storage:      nats.MemoryStorage, // 使用内存存储，测试更快
			Replicas:     1,                  // 单副本
			MaxMsgSize:   4 * 1024 * 1024,    // 默认最大消息大小为4MB
			Duplicates:   30 * time.Second,   // 重复消息检测窗口
			MaxConsumers: 100,                // 最大消费者数量
		}

		slog.Info("creating new jetstream stream",
			"name", n.cfg.StreamName,
			"subject", streamSubject,
			"retention", "workqueue",
			"storage", "memory",
			"max_age", n.cfg.MessageRetention)

		streamInfo, err = js.AddStream(streamConfig)
		if err != nil {
			lastErr = err
			slog.Error("failed to create stream", "error", err, "attempt", i+1)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		slog.Info("jetstream stream created successfully",
			"name", streamInfo.Config.Name,
			"subject", streamInfo.Config.Subjects[0])
		break
	}

	if streamInfo == nil {
		if lastErr != nil {
			return fmt.Errorf("failed to create or find stream after retries: %w", lastErr)
		}
		return errors.New("failed to create or find stream after retries")
	}

	// 如果流已存在但没有我们需要的主题，添加主题
	// 这在Stream已经存在但主题模式不同时发生
	streamSubjectPattern := streamSubject + ".>"
	found := false
	for _, subject := range streamInfo.Config.Subjects {
		if subject == streamSubjectPattern {
			found = true
			break
		}
	}

	// 如果需要更新流配置，添加新的主题
	if !found {
		updatedSubjects := append(streamInfo.Config.Subjects, streamSubjectPattern)
		streamInfo.Config.Subjects = updatedSubjects

		slog.Info("updating stream with new subject",
			"stream", n.cfg.StreamName,
			"new_subject", streamSubjectPattern)

		_, err = js.UpdateStream(&streamInfo.Config)
		if err != nil {
			slog.Error("failed to update stream subjects", "error", err)
			return fmt.Errorf("failed to update stream with new subject: %w", err)
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

	slog.Info("closing nats message bus...")

	// 标记为已关闭
	n.closed = true

	// 先关闭所有停止通道，通知处理goroutine退出
	for topic, stopCh := range n.stopChans {
		slog.Debug("closing stop channel for subscription", "topic", topic)
		close(stopCh)
		delete(n.stopChans, topic)
	}

	// 等待一小段时间，确保goroutine有时间响应停止信号
	time.Sleep(200 * time.Millisecond)

	// 关闭所有订阅
	for topic, sub := range n.subs {
		// 对于JetStream订阅使用Drain，对于标准NATS使用Unsubscribe
		if n.cfg.UseJetStream && n.js != nil {
			slog.Debug("draining jetstream subscription", "topic", topic)
			if err := sub.Drain(); err != nil {
				slog.Error("failed to drain subscription during close", "topic", topic, "error", err)
			}
		} else {
			slog.Debug("unsubscribing nats subscription", "topic", topic)
			if err := sub.Unsubscribe(); err != nil {
				slog.Error("failed to unsubscribe during close", "topic", topic, "error", err)
			}
		}
	}

	// 给JetStream的Drain操作更多时间完成
	if n.cfg.UseJetStream && n.js != nil {
		drainWait := 500 * time.Millisecond
		slog.Debug("waiting for jetstream drain to complete", "wait_ms", drainWait.Milliseconds())
		time.Sleep(drainWait)
	}

	// 清空订阅映射
	subsCount := len(n.subs)
	n.subs = make(map[string]*nats.Subscription)
	n.stopChans = make(map[string]chan struct{})

	// 尝试使用Drain关闭连接，这样可以让所有挂起的操作完成
	var closeErr error
	drainTimeout := 1 * time.Second

	// 如果配置了超时时间，使用配置的时间
	if n.cfg.OpTimeout > 0 {
		drainTimeout = n.cfg.OpTimeout * 10
		if drainTimeout > 5*time.Second {
			drainTimeout = 5 * time.Second
		}
	}

	slog.Debug("draining nats connection", "timeout_ms", drainTimeout.Milliseconds())
	if err := n.conn.Drain(); err != nil {
		slog.Error("error during nats connection drain", "error", err)
		closeErr = err

		// 如果Drain失败，尝试直接关闭
		n.conn.Close()
	} else {
		// 等待Drain完成指定的超时时间
		time.Sleep(drainTimeout)
	}

	slog.Info("nats bus closed", "subscriptions_cleaned", subsCount)
	return closeErr
}

// 确保NatsBus实现了MessageBus接口
var _ bus.MessageBus = (*NatsBus)(nil)
