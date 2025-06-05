package redis

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// publishErrorsCounter 记录发布错误次数
	publishErrorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gohub_bus_redis_publish_errors_total",
			Help: "Redis消息总线发布错误总数",
		},
	)

	// subscribeErrorsCounter 记录订阅错误次数
	subscribeErrorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gohub_bus_redis_subscribe_errors_total",
			Help: "Redis消息总线订阅错误总数",
		},
	)

	// reconnectsCounter 记录重连次数
	reconnectsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gohub_bus_redis_reconnects_total",
			Help: "Redis消息总线重连次数",
		},
	)

	// subscribeLatency 记录订阅消息延迟
	subscribeLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gohub_bus_redis_latency_seconds",
			Help:    "Redis消息总线订阅延迟(秒)",
			Buckets: prometheus.DefBuckets,
		},
	)
)

// 注册指标
func init() {
	// 注册Prometheus指标
	prometheus.MustRegister(publishErrorsCounter)
	prometheus.MustRegister(subscribeErrorsCounter)
	prometheus.MustRegister(reconnectsCounter)
	prometheus.MustRegister(subscribeLatency)
}

// IncPublishErrors 增加发布错误计数
func (r *RedisBus) IncPublishErrors() {
	publishErrorsCounter.Inc()
}

// IncSubscribeErrors 增加订阅错误计数
func (r *RedisBus) IncSubscribeErrors() {
	subscribeErrorsCounter.Inc()
}

// IncReconnects 增加重连计数
func (r *RedisBus) IncReconnects() {
	reconnectsCounter.Inc()
	atomic.AddUint64(&r.reconnects, 1)
}

// ObserveSubscribeLatency 观察订阅延迟
func (r *RedisBus) ObserveSubscribeLatency(d time.Duration) {
	subscribeLatency.Observe(d.Seconds())
}

// GetReconnectCount 获取重连次数
func (r *RedisBus) GetReconnectCount() uint64 {
	return atomic.LoadUint64(&r.reconnects)
}
