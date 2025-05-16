---
## ⚙️ 总览

| 阶段     | 模块                 | 目标                                    | 预计工时  |
| ------ | ------------------ | ------------------------------------- | ----- |
| **P0** | 目录&接口基线            | `bus/` 目录 + `MessageBus` 抽象 + NoopBus | 0.5 d |
| **P1** | Redis 连接层          | 连接配置、重连、Publish 单元测试                  | 1 d   |
| **P2** | Redis 订阅层          | ChanSubscribe + goroutine 退出钩子        | 1 d   |
| **P3** | NATS Core          | Core Pub/Sub + reconnect 回调           | 1.5 d |
| **P4** | NATS JetStream 可选层 | Durable consumer + Ack                | 1 d   |
| **P5** | 监控 & 文档            | Prom 指标、README、Godoc                  | 0.5 d |

> **原则**：下一个阶段只能在前一阶段 PR 合并并通过 CI 后开始。PR **绿色** = lint 0、单测全过、≥80 % cover，且“docs updated”。

---

## P0 – 基准目录与接口

### 🎯 目标

1. 固定 `internal/bus` 目录结构。
2. 写 `MessageBus` 抽象和 **NoopBus**（单机使用）。

### ✅ 任务清单

* [ ] 建立 `internal/bus/bus.go` 并声明接口
* [ ] 实现 `noopBus`（直接丢弃消息）
* [ ] 在 `hub` 注入 `bus.MessageBus`（接口倒置）
* [ ] 单元测试：`TestNoopBus_PublishSubscribe`

### 📂 交付物

```
internal/bus/
  |-- bus.go          // interface + Handler type
  |-- noop/
        noop.go
        noop_test.go
```

### 🔎 验收

```bash
go test ./internal/bus/... -run TestNoopBus -cover
```

---

## P1 – **RedisBus** 连接与 Publish

### 🎯 目标

* 建立到 Redis 的连接管理，支持 Sentinel/Cluster HA 配置 ([Redis][3])。
* 完成 `Publish`，在网络失效时 50 ms 超时返回 err。

### ✅ 任务

* [ ] 依赖 `github.com/redis/go-redis/v9` ([plutora.com][1])；写 `NewRedisBus(opts)`
* [ ] 连接参数来自 `Config.Bus.Redis`
* [ ] 自定义 `retryHook` 统计重连次数
* [ ] `Publish` 实现 + 超时 ctx
* [ ] 单元测试用 **miniredis** ([GitHub][4])：① 正常发布；② 关闭服务器后 Publish 返回 `ErrPublishTimeout`
* [ ] 指标：`gohub_bus_redis_publish_errors_total` Counter

### 📂 交付物

```
internal/bus/redis/
  |-- redis.go
  |-- publish.go
  |-- redis_test.go      // 使用 miniredis
```

### 🔎 验收脚本

```bash
go test ./internal/bus/redis -run TestPublishTimeout
```

---

## P2 – **RedisBus** 订阅层

### 🎯 目标

* 完成 `Subscribe`，保证 goroutine 可通过 CloseFunc 正常退出；Publish→Subscribe 基本往返。
* 支持“重连后自动恢复订阅”。

### ✅ 任务

* [ ] 调用 `rds.Subscribe(ctx, channel)` 创建 PS 对象
* [ ] 背景 goroutine 读取 `ps.Channel()`，回调用户 Handler
* [ ] `CloseFunc` 关闭 quit chan + `ps.Close()`
* [ ] 测试：miniredis + Subscribe；关闭 server → 启动新 server → 验证自动恢复
* [ ] 指标：`gohub_bus_redis_latency_seconds` Histogram【使用 `prometheus/client_golang` 指南 ([Prometheus][5])】

### 📂 交付物

```
internal/bus/redis/
  |-- subscribe.go
  |-- subscribe_test.go
```

### 🔎 验收

```bash
scripts/e2e_redis.sh   # 使用 docker-compose：两 Hub + Redis
```

---

## P3 – **NatsBus Core**

### 🎯 目标

* implement Core Pub/Sub (at-most-once)，含重连回调指标 `gohub_bus_nats_reconnects_total`。

### ✅ 任务

* [ ] `NewNatsBus(urls []string)` with `MaxReconnect = -1`、`ReconnectWait = 2s` ([docs.nats.io][6])
* [ ] `Publish()` → `nc.Publish()`；10 ms 超时
* [ ] `Subscribe()` → `nc.ChanSubscribe()`
* [ ] 单测使用 `nats-server -p 0` 子进程
* [ ] 覆盖重连场景：Kill server → restart → 重新收到消息

### 📂 交付物

```
internal/bus/nats/
  |-- nats.go
  |-- core_pubsub.go
  |-- core_test.go
```

---

## P4 – **JetStream** 可选持久化

### 🎯 目标

* 在 `bus.nats.jetstream=true` 时切 JetStream：实现 Durable Consumer + 手动 Ack **确保 at-least-once** ([docs.nats.io][7])。

### ✅ 任务

* [ ] 检查/创建 Stream (`js.AddStream`)
* [ ] `Publish()` → `js.Publish(ch, data)` + `PubAck` 超时检查 ([docs.nats.io][7])
* [ ] `Subscribe()` → `js.PullSubscribe()` + `Fetch` + `Ack()`
* [ ] 指标：`gohub_bus_nats_ack_pending` Gauge (`sub.Pending()`)
* [ ] 集成测试脚本验证 **宕机后消息不丢**

---

## P5 – 监控、CI、文档

### 🎯 目标

* 把上面所有指标注册进 `/metrics`；更新 README & Godoc；CI 覆盖率插件集成 ([GitHub][8])。

### ✅ 任务

* [ ] `metrics/bus.go` 统一注册
* [ ] GitHub Actions：`go test -coverprofile` + `go-test-coverage` Action
* [ ] README：新增 Redis / NATS 运行示例
* [ ] `docs/bus_spec.md` —— 生成于本计划

---

## 📑 参考资料一览

* Incremental /模块化开发 ([plutora.com][1], [GeeksforGeeks][2])
* Go 项目布局规范 ([GitHub][9])
* Redis Pub/Sub 无可靠投递保证 ([Stack Overflow][10])
* miniredis 单元测试利器 ([GitHub][4])
* Prometheus Go 仪表指南 ([Prometheus][5])
* GitHub Actions 覆盖率示例 ([GitHub][8])
* Go 上下文首参约定讨论 ([Stack Overflow][11])
* NATS bench 性能基准 ([docs.nats.io][6])
* Redis Sentinel HA 文档 ([Redis][3])
* JetStream Durable & Ack 机制 ([docs.nats.io][7])
* Redis / NATS 重连策略官方说明 ([docs.nats.io][6])

---

### ✉️ 如何使用这份计划

1. **复制 P0 任务块** 作为 AI 提示，让其创建接口与 NoopBus。
2. 合并后继续投喂 **P1**，如此循环。
3. 每阶段 CI 绿色后再进入下一阶段，确保质量递增。

如果某个阶段仍嫌“粒度太大”，请指出具体函数或测试，再继续拆分！
