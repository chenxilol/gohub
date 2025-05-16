# GoHub NATS消息总线实现

本项目实现了基于NATS的分布式消息总线，用于在Hub节点间可靠传递消息。

## 功能特点

### 核心功能

- **标准发布/订阅**: 实现基础的异步消息发布和订阅
- **JetStream持久化**: 支持消息持久化，确保消息可靠投递
- **消息确认机制**: 支持消息确认(Ack)和失败重试(Nak)
- **自动重连**: 在网络故障时自动重连NATS服务器
- **延迟重试**: 配置消息失败重试等待时间
- **监控指标**: 集成Prometheus监控，包括错误计数、消息延迟等

### 可靠性保障

- **至少一次投递**: 通过JetStream确保消息不丢失
- **超时处理**: 发布和订阅操作具有超时保护
- **错误处理**: 全面的错误检测和处理机制
- **重试策略**: 消息处理失败时自动重试

## 项目结构

```
internal/bus/
├── bus.go              # 消息总线接口定义
└── nats/               # NATS实现
    ├── nats.go         # 基础结构和连接管理
    ├── core_pubsub.go  # 发布订阅核心逻辑
    ├── metrics.go      # Prometheus指标定义
    ├── core_test.go    # 核心功能测试
    ├── jetstream_test.go # JetStream功能测试
    └── publish_test.go # 发布专项测试
```

## 安装与测试

### 安装NATS服务器

在运行测试前，需要先安装NATS服务器：

```bash
# 执行自动安装脚本
./scripts/install_nats.sh
```

### 运行测试

```bash
# 执行全部测试
./scripts/test_bus.sh

# 也可以只运行特定部分的测试
go test -v ./internal/bus/nats -run TestNatsBus_JetStream
```

## 使用示例

### 创建消息总线实例

```go
import (
    "gohub/internal/bus/nats"
)

// 创建配置
cfg := nats.DefaultConfig()
cfg.URLs = []string{"nats://localhost:4222"}
cfg.UseJetStream = true // 启用JetStream持久化

// 创建消息总线
bus, err := nats.New(cfg)
if err != nil {
    log.Fatal(err)
}
defer bus.Close()
```

### 发布消息

```go
// 发布消息到主题
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

err := bus.Publish(ctx, "my-topic", []byte("hello world"))
if err != nil {
    log.Printf("发布失败: %v", err)
}
```

### 订阅消息

```go
// 订阅主题
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

ch, err := bus.Subscribe(ctx, "my-topic")
if err != nil {
    log.Printf("订阅失败: %v", err)
    return
}

// 处理接收到的消息
for msg := range ch {
    log.Printf("收到消息: %s", string(msg))
}
```

## 性能调优

可以通过调整以下配置参数优化性能：

- `OpTimeout`: 操作超时时间，影响发布和接收速度
- `ReconnectWait`: 重连等待时间，影响故障恢复速度
- `MessageRetention`: JetStream消息保留时间
- `MaxDeliver`: 消息最大重试次数 