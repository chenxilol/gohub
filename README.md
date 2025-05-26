# GoHub - 基于开源框架的高性能分布式 Go WebSocket 框架

[![Go 版本](https://img.shields.io/badge/go%20version-%3E%3D1.20-6F93CF.svg)](https://golang.org/dl/)
[![许可证: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**GoHub** 是一个基于Gorilla WebSocket开发的、强大的分布式WebSocket框架，专为高并发实时通信系统设计。其核心优势在于**完全分布式架构设计**和**高效的消息分发机制**，可轻松扩展至数十万并发连接，同时保持高吞吐量和低延迟。

## 🌟 核心亮点

### 1️⃣ 完全分布式设计
- **无状态节点扩展**: 每个GoHub节点都是无状态的，可以随时添加或移除，实现真正的水平扩展
- **多种消息总线选择**: 
  - 内置支持NATS（包括JetStream持久化）和Redis作为高性能消息总线
  - 节点间实时通信确保消息在集群中准确传递
- **消息去重机制**: 确保在分布式环境下消息的精确一次处理
- **集群状态同步**: 房间信息和客户端状态在集群中自动同步

### 2️⃣ 高效消息分发系统
- **基于类型的中央分发器**: 根据消息类型智能路由到对应处理器
- **自定义消息处理**: 轻松扩展框架处理业务特定消息类型
- **广播与定向消息**: 支持一对一、一对多、多对多等多种通信模式
- **房间/频道机制**: 强大的房间管理功能，支持动态创建、加入、离开和消息广播

## ✨ 其他主要功能

* **高级房间/频道管理**:
    * 创建、删除和列出房间，支持跨节点房间成员管理
    * 允许客户端加入和离开房间，实时同步到所有节点
    * 在特定房间内广播消息，即使接收者分布在不同节点上
    * 支持每个房间的最大客户端数量限制

* **灵活的认证与授权**:
    * 基于 JWT 的认证机制
    * 细粒度的权限系统 (例如, `send:message`, `create:room`)
    * 可配置的匿名访问

* **可观测性**:
    * 集成 Prometheus 指标，用于监控连接数、消息量、房间数和错误
    * 使用 `log/slog` 进行结构化日志记录
    * 分布式跟踪支持，帮助定位跨节点问题

* **服务端 SDK**:
    * 提供便捷的 SDK，用于将 GoHub 功能集成到您的业务逻辑中
    * 事件驱动：可订阅客户端连接、断开、消息和房间事件

* **配置管理**:
    * 通过 Viper 使用 YAML 文件和环境变量进行灵活配置
    * 支持配置热加载

* **高性能**: 
    * 单节点支持10万+并发连接
    * 集群模式可扩展至百万级连接
    * 优化的内存使用，减少GC压力

## 🚀 快速开始

### 环境要求

* Go 1.20 或更高版本
* (用于分布式模式) NATS Server (推荐 v2.8+，使用 JetStream 以支持持久化) 或 Redis

### 安装

1. **克隆仓库:**
   ```bash
   git clone https://github.com/chenxilol/gohub.git
   cd gohub
   ```

2. **下载依赖:**
   ```bash
   go mod tidy
   ```

### 配置

1. **复制示例配置文件:**
   ```bash
   cp configs/config.example.yaml configs/config.yaml
   ```

2. **编辑 `configs/config.yaml`:**
   * 对于**分布式集群模式**（推荐用于生产环境）:
     ```yaml
     cluster:
       enabled: true
       bus_type: "nats"  # 或 "redis"
       nats:
         url: "nats://localhost:4222"
         # 如果使用JetStream持久化
         stream_name: "gohub"
         durable_name: "gohub-durable"
     ```
   * 对于无需外部依赖的快速**单节点启动**:
     ```yaml
     cluster:
       enabled: false
       bus_type: "noop"
     ```
   * 更新 `auth.secret_key` 用于 JWT 生成

   *请参考 `configs/config.example.yaml` 查看所有可用选项*

### 运行 GoHub

1. **从源码运行:**
   ```bash
   go run cmd/gohub/main.go -config configs/config.yaml
   ```
   默认情况下，服务将在 `config.yaml` 中指定的地址启动 (例如：`:8080`)。WebSocket 端点位于 `/ws`。

2. **使用 Docker 集群模式 (生产环境推荐):**
   ```bash
   # 启动完整的分布式环境
   docker-compose up -d
   ```

## 💻 分布式WebSocket使用示例

### 1. 配置多节点集群

假设我们要设置一个3节点的GoHub集群：

```bash
# 节点1
go run cmd/gohub/main.go -config configs/node1.yaml

# 节点2
go run cmd/gohub/main.go -config configs/node2.yaml

# 节点3
go run cmd/gohub/main.go -config configs/node3.yaml
```

各节点配置文件中需要指向相同的NATS或Redis服务：

```yaml
# node1.yaml, node2.yaml, node3.yaml 中的共同配置
cluster:
  enabled: true
  bus_type: "nats"
  nats:
    url: "nats://localhost:4222"
    stream_name: "gohub"
```

### 2. 建立WebSocket连接

前端JavaScript示例：

```javascript
// 连接到任意GoHub节点
const socket = new WebSocket("ws://node1:8080/ws");  // 可以是集群中任意节点

socket.onopen = function(e) {
  console.log("WebSocket连接已建立");
  
  // 发送认证消息
  const authMessage = {
    message_type: "authenticate",
    data: {
      token: "您的JWT令牌"
    }
  };
  socket.send(JSON.stringify(authMessage));
};

socket.onmessage = function(event) {
  const message = JSON.parse(event.data);
  console.log("收到消息:", message);
  
  // 处理消息...
};
```

### 3. 房间功能跨节点工作

即使用户连接到不同节点，GoHub的分布式特性也能确保房间功能正常工作：

```javascript
// 用户A（连接到节点1）创建并加入房间
function joinRoom(roomId) {
  socket.send(JSON.stringify({
    message_id: Date.now(),
    message_type: "join_room",
    data: { room_id: roomId }
  }));
}

// 用户B（连接到节点2）也可以加入同一房间并发送消息
// 消息会通过消息总线同步到所有节点
function sendRoomMessage(roomId, content) {
  socket.send(JSON.stringify({
    message_id: Date.now(),
    message_type: "room_message",
    data: {
      room_id: roomId,
      content: content
    }
  }));
}
```

## 📊 与其他WebSocket框架对比

| 特性 | GoHub | Melody | Gorilla WebSocket | go-netty-ws |
|------|-------|--------|-------------------|-------------|
| **分布式支持** | ✅ 完全内置 | ❌ 无 | ❌ 无 | ⚠️ 有限 |
| **跨节点消息传递** | ✅ NATS/Redis | ❌ 需自行实现 | ❌ 需自行实现 | ⚠️ 有限 |
| **消息分发机制** | ✅ 类型化路由 | ❌ 简单回调 | ❌ 基础接口 | ⚠️ 有限 |
| 房间/频道管理 | ✅ 内置 | ⚠️ 有限支持 | ❌ 需自行实现 | ❌ 需自行实现 |
| 认证与授权 | ✅ JWT + 细粒度权限 | ❌ 需自行实现 | ❌ 需自行实现 | ❌ 需自行实现 |
| 指标监控 | ✅ Prometheus | ❌ 需自行实现 | ❌ 需自行实现 | ❌ 需自行实现 |
| SDK支持 | ✅ 服务端 | ❌ 无 | ❌ 无 | ⚠️ 有限 |
| 活跃维护 | ✅ 是 | ⚠️ 有限 | ✅ 是 | ⚠️ 有限 |

## 🚀 性能基准

GoHub在分布式模式下依然保持出色性能：

| 配置 | 连接数量 | 内存使用 | 每秒消息处理能力 |
|------|--------|---------|----------------|
| 单节点 | 10,000 | ~281MB | >10,000 |
| 单节点 | 100,000 | ~2.7GB | >5,000 |
| 3节点集群 | 300,000 | ~8.1GB (总计) | >15,000 (总计) |
| 5节点集群 | 500,000 | ~13.5GB (总计) | >25,000 (总计) |

*注意：实际性能可能因硬件配置、消息大小和业务逻辑复杂度而异*

## 消息格式

GoHub 使用基于 JSON 的消息格式：

```json
{
  "message_id": 123,          // 可选: 消息的唯一 ID (客户端生成，用于请求/响应关联)
  "message_type": "your_type", // 字符串: 消息类型 (例如："ping", "join_room", "custom_action")
  "data": { ... },            // 对象: 消息的有效负载，其结构取决于 message_type
  "request_id": 456           // 可选: 如果这是一条响应消息，它可能对应原始请求的 message_id
}
```

关于内置消息类型（如 `ping`, `join_room`, `leave_room`, `room_message`, `direct_message`）的示例，请参考 `internal/handlers/handlers.go`。

## 🛠️ API 端点

除了 `/ws` WebSocket 端点外，GoHub 还暴露了一些 HTTP API 端点：

* **`/metrics`**: Prometheus 指标，用于监控
* **`/health`**: 健康检查端点，返回服务器状态和版本
* **`/api/broadcast` (POST)**: 向所有连接的客户端广播消息，跨集群节点
  * 请求体: `{"message": "your message content"}`
* **`/api/stats` (GET)**: 获取服务器统计信息 (客户端数量、房间数量)
* **`/api/rooms` (GET, POST)**:
  * GET: 列出所有房间及其成员数量
  * POST: 创建一个新房间。请求体: `{"id": "room_id", "name": "Room Name", "max_clients": 100}`
* **`/api/clients` (GET)**: 获取客户端统计信息

## 🏗️ 项目结构

GoHub 遵循标准的 Go 项目布局：

* **`cmd/gohub/`**: 主应用程序入口和服务器设置
* **`configs/`**: 配置文件和加载逻辑
* **`internal/`**: 核心内部包：
  * `auth/`: 认证 (JWT) 和授权逻辑
  * `bus/`: 消息总线接口和实现 (NATS, Redis, NoOp)
  * `dispatcher/`: 将传入的 WebSocket 消息路由到适当的处理器
  * `handlers/`: WebSocket 消息的默认处理器
  * `hub/`: 管理 WebSocket 客户端、房间和消息流
  * `metrics/`: Prometheus 指标收集
  * `sdk/`: 用于业务逻辑集成的服务端 SDK
  * `utils/`: 通用工具函数
  * `websocket/`: WebSocket 连接适配器接口和实现
* **`scripts/`**: 工具和测试脚本

## 🔧 扩展 GoHub (自定义消息处理器)

您可以为特定于应用程序的 WebSocket 消息添加自定义处理器。

**当前推荐方法:**

GoHub 使用一个中心的、单例的分发器。要在不修改 GoHub 内部代码的情况下添加自定义消息处理器，您可以在您的 `main.go` (或应用程序设置代码) 中，在 GoHub 服务器完全启动*之前*，将它们注册到这个全局分发器实例。

**示例 (在您的 `cmd/gohub/main.go` 中进行概念性修改):**

```go
// 将此代码放在调用 NewGoHubServer 和 server.Start() 之前

import (
	"gohub/internal/dispatcher"
	"gohub/internal/hub" // 用于 hub.Client 类型和 hub.Frame
	"gohub/internal/websocket" // 用于消息类型如 websocket.TextMessage
	"encoding/json"
	"context"
	"log/slog"
	// ... 其他必要的导入
)

// 1. 定义您的自定义处理函数
//    它应该匹配 dispatcher.HandlerFunc 签名
func handleMyCustomAction(ctx context.Context, client *hub.Client, data json.RawMessage) error {
    slog.Info("正在处理 'my_custom_action'", "client_id", client.ID(), "payload", string(data))
    
    var requestPayload struct {
        // 定义您的自定义消息数据中的预期字段
        ActionDetail string `json:"action_detail"`
    }
    if err := json.Unmarshal(data, &requestPayload); err != nil {
        slog.Error("解析自定义操作负载失败", "error", err, "client_id", client.ID())
        // 可选：向客户端发送错误响应
        errorResp := hub.NewError(hub.ErrCodeInvalidFormat, "my_custom_action 的负载无效", 0, err.Error()) // 假设在没有可用请求 ID 时使用 0
        errorMsgJson, _ := errorResp.ToJSON()
        _ = client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: errorMsgJson})
        return err 
    }

    slog.Info("自定义操作详情", "client_id", client.ID(), "detail", requestPayload.ActionDetail)

    // ... 您的业务逻辑代码 ...

    // 示例：发送成功回复
    replyData := map[string]interface{}{"status": "success", "action_processed": "my_custom_action", "detail_received": requestPayload.ActionDetail}
    // 为回复构建一个 hub.Message
    // 假设您有生成消息 ID 的方法，或者这是一个服务器推送，没有显式的 request_id 匹配
    responseHubMsg, err := hub.NewMessage(0, "my_custom_action_reply", replyData) // 使用 0 作为占位消息 ID
    if err != nil {
         slog.Error("创建回复消息失败", "error", err, "client_id", client.ID())
        return err
    }
    encodedReply, err := responseHubMsg.Encode()
    if err != nil {
        slog.Error("编码回复消息失败", "error", err, "client_id", client.ID())
        return err
    }
    
    return client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: encodedReply})
}

// 在您的 main 函数中:
func main() {
    // ... (现有设置：flag.Parse(), 加载配置, 设置日志记录器) ...
	config, err := configs.LoadConfig(*configFile) // 示例
	// ... (处理错误) ...
	// 根据 config.Log.Level 设置日志记录器
	// ...

    // 2. 获取全局分发器实例
    d := dispatcher.GetDispatcher() //

    // 3. 注册您的自定义处理器
    d.Register("my_custom_action", handleMyCustomAction)
    slog.Info("已注册自定义消息处理器。")

    // 初始化并启动 GoHub 服务器
    // NewGoHubServer 内部会为内置类型调用 handlers.RegisterHandlers，
    // 这会添加到同一个分发器实例中。
    server, err := NewGoHubServer(&config) //
    if err != nil {
        slog.Error("创建服务器失败", "error", err)
        os.Exit(1)
    }
    
    // ... (main 函数的其余部分：优雅关闭, server.Start()) ...
}
```
*未来版本的 GoHub SDK 可能会提供更直接的处理器注册方法，以实现更封装的方式。*

## 🧪 测试

GoHub 包含一套全面的测试：

* **单元测试**: 与代码一起位于 `_test.go` 文件中 (例如, `internal/bus/nats/nats_test.go`)
* **集成测试**: 位于 `internal/integration/` 和 `cmd/gohub/` 的部分内容，用于服务器级测试
* **压力测试**: `cmd/gohub/stress_test.go` 评估高负载下的性能
* **分布式测试**: `cmd/gohub/redis_distributed_test.go` 专门测试使用 Redis 的集群功能
* **Shell 脚本**: `scripts/` 目录包含用于运行各种测试的脚本
  * `scripts/test_nats.sh`: 测试 NATS 功能
  * `test_broadcast.sh` / `test_redis_broadcast.sh`: 端到端广播测试

运行测试 (假设相关测试依赖如 NATS/Redis 可用):
```bash
go test ./...
# 或者使用 scripts/ 目录下的特定脚本，例如：
./scripts/test_bus.sh 
```

## 💡 常见问题解答

### 1. GoHub与基础框架Gorilla WebSocket的关系？

GoHub建立在Gorilla WebSocket之上，Gorilla提供了基础的WebSocket协议实现，而GoHub则提供了完整的分布式架构、房间管理、消息分发和集群通信等企业级功能。GoHub可以看作是对Gorilla WebSocket的一个高级抽象和功能扩展，使其更适合构建复杂的实时应用。

### 2. 如何实现跨节点的消息传递？

GoHub使用消息总线（NATS或Redis）在集群节点间传递消息。当一个节点需要向连接到其他节点的客户端发送消息时，它会通过消息总线广播这个消息。其他节点会接收到这个消息，并将其转发给相应的客户端。整个过程对开发者透明，您只需正常使用API，不需要关心客户端连接在哪个节点上。

### 3. GoHub的分布式架构如何提高系统可靠性？

GoHub的分布式设计带来几个关键优势：
- **高可用性**：单个节点故障不会影响整个系统
- **水平扩展**：可以通过添加更多节点来增加系统容量
- **负载均衡**：连接可以分散到多个节点上
- **地理分布**：节点可以部署在不同区域，减少延迟

### 4. 如何监控集群状态？

GoHub通过Prometheus指标提供全面的监控能力，包括每个节点的连接数、消息处理量、房间数量等。您可以使用Grafana创建仪表板来可视化这些指标，实时监控集群健康状况。

## 🤝 贡献

欢迎贡献！请随时提交 Pull Request 或开启 Issue。

在贡献之前，请：
1.  确保您的代码符合 Go 的最佳实践和项目现有风格。
2.  为您的更改编写或更新测试。
3.  使用 `go fmt` 或 `gofumpt` 格式化您的代码。
4.  考虑运行 `go vet` 和 `golangci-lint` (如果提供了配置文件)。

## 📜 许可证

GoHub 使用 MIT 许可证。详情请参阅仓库根目录下的 `LICENSE` 文件。 