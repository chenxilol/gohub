// Package metrics 提供监控指标收集功能
package metrics

import (
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	once           sync.Once
	registry       *prometheus.Registry
	defaultMetrics *Metrics
)

// Metrics 封装所有监控指标
type Metrics struct {
	// 连接指标
	ConnectedClients  prometheus.Gauge
	ConnectionRate    prometheus.Counter
	DisconnectionRate prometheus.Counter

	// 消息指标
	MessagesTotal  prometheus.Counter
	MessageSize    prometheus.Histogram
	MessageRateIn  prometheus.Counter
	MessageRateOut prometheus.Counter

	// 房间指标
	ActiveRooms  prometheus.Gauge
	RoomJoins    prometheus.Counter
	RoomLeaves   prometheus.Counter
	RoomMessages prometheus.Counter

	// 延迟指标
	MessageLatency prometheus.Histogram

	// 错误指标
	ErrorsTotal         prometheus.Counter
	CriticalErrorsTotal prometheus.Counter

	// 认证指标
	AuthSuccess prometheus.Counter
	AuthFailure prometheus.Counter
}

// NewMetrics 创建新的Metrics实例
func NewMetrics(namespace string) *Metrics {
	registry = prometheus.NewRegistry()

	metrics := &Metrics{
		// 连接指标
		ConnectedClients: promauto.With(registry).NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "connected_clients",
			Help:      "当前连接的客户端总数",
		}),
		ConnectionRate: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "connection_rate",
			Help:      "新连接速率",
		}),
		DisconnectionRate: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "disconnection_rate",
			Help:      "断开连接速率",
		}),

		// 消息指标
		MessagesTotal: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "messages_total",
			Help:      "处理的消息总数",
		}),
		MessageSize: promauto.With(registry).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "message_size",
			Help:      "消息大小分布",
			Buckets:   []float64{64, 256, 1024, 4096, 16384, 65536, 262144},
		}),
		MessageRateIn: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "message_rate_in",
			Help:      "入站消息速率",
		}),
		MessageRateOut: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "message_rate_out",
			Help:      "出站消息速率",
		}),

		// 房间指标
		ActiveRooms: promauto.With(registry).NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "active_rooms",
			Help:      "当前活跃房间数",
		}),
		RoomJoins: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "room_joins",
			Help:      "加入房间操作计数",
		}),
		RoomLeaves: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "room_leaves",
			Help:      "离开房间操作计数",
		}),
		RoomMessages: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "room_messages",
			Help:      "房间消息计数",
		}),

		// 延迟指标
		MessageLatency: promauto.With(registry).NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "message_latency",
			Help:      "消息处理延迟(毫秒)",
			Buckets:   prometheus.DefBuckets,
		}),

		// 错误指标
		ErrorsTotal: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "errors_total",
			Help:      "错误总数",
		}),
		CriticalErrorsTotal: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "critical_errors_total",
			Help:      "严重错误总数",
		}),

		// 认证指标
		AuthSuccess: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "auth_success",
			Help:      "认证成功计数",
		}),
		AuthFailure: promauto.With(registry).NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "auth_failure",
			Help:      "认证失败计数",
		}),
	}

	return metrics
}

// GetRegistry 获取Prometheus注册表
func GetRegistry() *prometheus.Registry {
	return registry
}

// Default 获取默认指标实例
func Default() *Metrics {
	once.Do(func() {
		defaultMetrics = NewMetrics("gohub")
	})
	return defaultMetrics
}

// 便捷方法，用于快速记录指标

// ClientConnected 记录客户端连接
func ClientConnected() {
	m := Default()
	m.ConnectedClients.Inc()
	m.ConnectionRate.Inc()
}

// ClientDisconnected 记录客户端断开连接
func ClientDisconnected() {
	m := Default()
	m.ConnectedClients.Dec()
	m.DisconnectionRate.Inc()
}

// MessageReceived 记录收到消息
func MessageReceived(sizeBytes float64) {
	m := Default()
	m.MessagesTotal.Inc()
	m.MessageRateIn.Inc()
	m.MessageSize.Observe(sizeBytes)
}

// MessageSent 记录发送消息
func MessageSent(sizeBytes float64) {
	m := Default()
	m.MessageRateOut.Inc()
}

// RoomCreated 记录创建房间
func RoomCreated() {
	m := Default()
	m.ActiveRooms.Inc()
}

// RoomDeleted 记录删除房间
func RoomDeleted() {
	m := Default()
	m.ActiveRooms.Dec()
}

// RoomJoined 记录加入房间
func RoomJoined() {
	m := Default()
	m.RoomJoins.Inc()
}

// RoomLeft 记录离开房间
func RoomLeft() {
	m := Default()
	m.RoomLeaves.Inc()
}

// RoomMessageSent 记录房间消息
func RoomMessageSent() {
	m := Default()
	m.RoomMessages.Inc()
}

// RecordLatency 记录延迟
func RecordLatency(latencyMs float64) {
	m := Default()
	m.MessageLatency.Observe(latencyMs)
}

// RecordError 记录错误
func RecordError() {
	m := Default()
	m.ErrorsTotal.Inc()
}

// RecordAuthSuccess 记录认证成功
func RecordAuthSuccess() {
	m := Default()
	m.AuthSuccess.Inc()
}

// RecordAuthFailure 记录认证失败
func RecordAuthFailure() {
	m := Default()
	m.AuthFailure.Inc()
}

// RecordCriticalError 记录严重错误
func RecordCriticalError(errorType string) {
	m := Default()
	m.CriticalErrorsTotal.Inc()

	// 记录在日志中，便于排查
	slog.Error("critical error encountered", "type", errorType)
}
