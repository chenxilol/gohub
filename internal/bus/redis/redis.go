// Package redis 提供基于Redis的消息总线实现
package redis

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"gohub/internal/bus"

	"github.com/redis/go-redis/v9"
)

// Config Redis连接配置选项
type Config struct {
	// 连接地址 (单机模式、集群模式或哨兵模式)
	Addrs []string

	// 密码，如果需要的话
	Password string

	// 数据库编号 (仅单机模式和哨兵模式有效)
	DB int

	// 哨兵模式的主节点名称
	MasterName string

	// 连接池大小
	PoolSize int

	// 最小空闲连接数
	MinIdleConns int

	// 连接超时时间
	DialTimeout time.Duration

	// 读取超时时间
	ReadTimeout time.Duration

	// 写入超时时间
	WriteTimeout time.Duration

	// 重试间隔
	RetryInterval time.Duration

	// 最大重试次数
	MaxRetries int

	// 消息总线操作超时（发布超时）
	OpTimeout time.Duration

	// 键前缀
	KeyPrefix string

	// 模式: single(单机), sentinel(哨兵), cluster(集群)
	Mode string
}

func DefaultConfig() Config {
	return Config{
		Addrs:         []string{"localhost:6379"},
		Password:      "",
		DB:            0,
		MasterName:    "",
		PoolSize:      10,
		MinIdleConns:  2,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
		WriteTimeout:  3 * time.Second,
		RetryInterval: 200 * time.Millisecond,
		MaxRetries:    3,
		OpTimeout:     500 * time.Millisecond,
		KeyPrefix:     "gohub:",
		Mode:          "single", // 默认单机模式
	}
}

type RedisBus struct {
	client     redis.UniversalClient // 通用客户端接口，兼容单机、哨兵和集群模式
	cfg        Config
	mu         sync.RWMutex
	closed     bool
	subs       map[string]context.CancelFunc // 活跃订阅的取消函数
	reconnects uint64                        // 重连次数统计
}

// RetryHook 用于统计重试次数的钩子
type RetryHook struct {
	bus *RedisBus
}

func (rh *RetryHook) DialHook(hook redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return hook(ctx, network, addr)
	}
}

func (rh *RetryHook) ProcessHook(hook redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		return hook(ctx, cmd)
	}
}

func (rh *RetryHook) ProcessPipelineHook(hook redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return hook(ctx, cmds)
	}
}

// OnRetry 记录每次重连事件
func (rh *RetryHook) OnRetry(ctx context.Context, cmd redis.Cmder, attempt int, err error) {
	if rh.bus != nil {
		atomic.AddUint64(&rh.bus.reconnects, 1)
		slog.Warn("redis reconnecting", "reconnects", rh.bus.reconnects, "attempt", attempt, "error", err, "cmd", cmd.Name())
	}
}

func New(cfg Config) (*RedisBus, error) {
	var client redis.UniversalClient

	opts := &redis.UniversalOptions{
		Addrs:        cfg.Addrs,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		MaxRetries:   cfg.MaxRetries,
	}

	// 根据模式创建不同类型的客户端
	switch cfg.Mode {
	case "sentinel":
		opts.MasterName = cfg.MasterName
		client = redis.NewUniversalClient(opts)
	case "cluster":
		client = redis.NewUniversalClient(opts)
	default: // "single" 或其他情况默认为单节点模式
		// 确保有至少一个地址
		if len(cfg.Addrs) == 0 {
			cfg.Addrs = []string{"localhost:6379"}
		}
		client = redis.NewUniversalClient(opts)
	}

	// 创建总线实例
	rb := &RedisBus{
		client: client,
		cfg:    cfg,
		subs:   make(map[string]context.CancelFunc),
	}

	// 添加重试钩子
	client.AddHook(&RetryHook{bus: rb})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), cfg.DialTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		slog.Error("failed to connect to redis", "error", err)
		return nil, err
	}

	slog.Info("connected to redis", "addrs", cfg.Addrs, "mode", cfg.Mode)
	return rb, nil
}

func (r *RedisBus) formatKey(topic string) string {
	return r.cfg.KeyPrefix + topic
}

// Close 实现MessageBus.Close，关闭Redis连接
func (r *RedisBus) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}

	// 标记为已关闭
	r.closed = true

	// 取消所有活跃的订阅
	for topic, cancel := range r.subs {
		cancel()
		delete(r.subs, topic)
	}

	// 关闭Redis连接
	return r.client.Close()
}

var _ bus.MessageBus = (*RedisBus)(nil)
