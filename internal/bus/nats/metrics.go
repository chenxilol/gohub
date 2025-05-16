package nats

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// publishErrorsCounter 记录发布错误次数
	publishErrorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gohub_bus_nats_publish_errors_total",
			Help: "NATS消息总线发布错误总数",
		},
	)

	// subscribeErrorsCounter 记录订阅错误次数
	subscribeErrorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gohub_bus_nats_subscribe_errors_total",
			Help: "NATS消息总线订阅错误总数",
		},
	)

	// ackErrorsCounter 记录确认消息错误次数
	ackErrorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gohub_bus_nats_ack_errors_total",
			Help: "NATS消息总线确认消息错误总数",
		},
	)

	// reconnectsCounter 记录重连次数
	reconnectsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "gohub_bus_nats_reconnects_total",
			Help: "NATS消息总线重连次数",
		},
	)

	// ackPendingGauge 记录等待确认的消息数量
	ackPendingGauge = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "gohub_bus_nats_ack_pending",
			Help: "NATS JetStream等待确认的消息数量",
		},
	)

	// publishLatency 记录发布消息延迟
	publishLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "gohub_bus_nats_publish_latency_seconds",
			Help:    "NATS消息总线发布延迟(秒)",
			Buckets: prometheus.DefBuckets,
		},
	)
)

// 注册指标
func init() {
	// 注册Prometheus指标
	prometheus.MustRegister(publishErrorsCounter)
	prometheus.MustRegister(subscribeErrorsCounter)
	prometheus.MustRegister(ackErrorsCounter)
	prometheus.MustRegister(reconnectsCounter)
	prometheus.MustRegister(ackPendingGauge)
	prometheus.MustRegister(publishLatency)
}

// IncPublishErrors 增加发布错误计数
func (n *NatsBus) IncPublishErrors() {
	publishErrorsCounter.Inc()
}

// IncSubscribeErrors 增加订阅错误计数
func (n *NatsBus) IncSubscribeErrors() {
	subscribeErrorsCounter.Inc()
}

// IncAckErrors 增加确认错误计数
func (n *NatsBus) IncAckErrors() {
	ackErrorsCounter.Inc()
}

// IncReconnects 增加重连计数
func (n *NatsBus) IncReconnects() {
	reconnectsCounter.Inc()
}

// UpdateAckPending 更新等待确认的消息数量
func (n *NatsBus) UpdateAckPending(count int) {
	ackPendingGauge.Set(float64(count))
}

// ObservePublishLatency 观察发布延迟
func (n *NatsBus) ObservePublishLatency(d time.Duration) {
	publishLatency.Observe(d.Seconds())
}
