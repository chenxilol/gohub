package bus

import (
	"context"
	"log/slog"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdConfig 配置etcd连接
type EtcdConfig struct {
	Endpoints   []string      // etcd服务器地址列表
	DialTimeout time.Duration // 连接超时
	KeyPrefix   string        // key前缀
}

// DefaultEtcdConfig 返回默认配置
func DefaultEtcdConfig() EtcdConfig {
	return EtcdConfig{
		Endpoints:   []string{"http://localhost:2379"},
		DialTimeout: 5 * time.Second,
		KeyPrefix:   "/gohub",
	}
}

// EtcdBus 基于etcd实现的消息总线
type EtcdBus struct {
	client *clientv3.Client
	cfg    EtcdConfig
	subs   map[string]chan []byte
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
}

// NewEtcdBus 创建新的etcd消息总线
func NewEtcdBus(cfg EtcdConfig) (*EtcdBus, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &EtcdBus{
		client: client,
		cfg:    cfg,
		subs:   make(map[string]chan []byte),
		ctx:    ctx,
		cancel: cancel,
		closed: false,
	}, nil
}

// formatKey 格式化完整key
func (b *EtcdBus) formatKey(topic string) string {
	return b.cfg.KeyPrefix + "/" + topic
}

// Publish 实现MessageBus.Publish
func (b *EtcdBus) Publish(ctx context.Context, topic string, data []byte) error {
	if b.closed {
		return ErrBusClosed
	}
	if topic == "" {
		return ErrTopicEmpty
	}

	key := b.formatKey(topic)
	// 使用5秒租约，避免消息永久存储
	lease, err := b.client.Grant(ctx, 5)
	if err != nil {
		slog.Error("etcd grant lease failed", "error", err)
		return err
	}

	_, err = b.client.Put(ctx, key, string(data), clientv3.WithLease(lease.ID))
	if err != nil {
		slog.Error("etcd put failed", "key", key, "error", err)
		return ErrPublishFailed
	}
	return nil
}

// Subscribe 实现MessageBus.Subscribe
func (b *EtcdBus) Subscribe(ctx context.Context, topic string) (<-chan []byte, error) {
	if b.closed {
		return nil, ErrBusClosed
	}
	if topic == "" {
		return nil, ErrTopicEmpty
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan []byte, 100)
	b.subs[topic] = ch

	// 启动watch协程
	go b.watchTopic(ctx, topic, ch)

	return ch, nil
}

// watchTopic 监视主题变化
func (b *EtcdBus) watchTopic(ctx context.Context, topic string, ch chan []byte) {
	key := b.formatKey(topic)
	watchCh := b.client.Watch(b.ctx, key)

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.ctx.Done():
			return
		case resp := <-watchCh:
			for _, event := range resp.Events {
				if event.Type == clientv3.EventTypePut {
					select {
					case ch <- event.Kv.Value:
					default:
						slog.Warn("channel buffer full, message dropped", "topic", topic)
					}
				}
			}
		}
	}
}

// Unsubscribe 实现MessageBus.Unsubscribe
func (b *EtcdBus) Unsubscribe(topic string) error {
	if topic == "" {
		return ErrTopicEmpty
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.subs[topic]; ok {
		close(ch)
		delete(b.subs, topic)
	}
	return nil
}

// Close 实现MessageBus.Close
func (b *EtcdBus) Close() error {
	if b.closed {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true
	b.cancel()

	// 关闭所有订阅
	for topic, ch := range b.subs {
		close(ch)
		delete(b.subs, topic)
	}

	return b.client.Close()
}

// 确保EtcdBus实现了MessageBus接口
var _ MessageBus = (*EtcdBus)(nil)
