
# GoHub - 高性能 Go WebSocket 框架

**GoHub** 是一个使用 Go 语言构建的、健壮的高性能 WebSocket 框架，专为实时通信应用设计。它提供了房间管理、认证与授权、可靠的消息传递以及分布式集群支持等功能，适用于聊天应用、实时协作工具、在线游戏后端等多种场景。

[![Go 版本](https://img.shields.io/badge/go%20version-%3E%3D1.20-6F93CF.svg)](https://golang.org/dl/)
[![许可证: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
## ✨ 功能特性

* **标准 WebSocket 通信**: 采用行业标准的 WebSocket 协议，支持文本和二进制消息。
* **高级房间/频道管理**:
    * 创建、删除和列出房间。
    * 允许客户端加入和离开房间。
    * 在特定房间内广播消息。
    * 支持每个房间的最大客户端数量限制。
* **灵活的认证与授权**:
    * 基于 JWT 的认证机制。
    * 细粒度的权限系统 (例如, `send:message`, `create:room`)。
    * 可配置的匿名访问。
* **消息分发与处理**:
    * 基于消息类型的中心化分发器。
    * 针对通用操作（ping、房间操作）的预定义处理器。
    * 易于扩展以支持自定义业务逻辑消息类型。
* **集群支持与可扩展性**:
    * 为分布式环境设计，集成了消息总线。
    * 支持 NATS (包括用于持久化的 JetStream) 和 Redis 作为消息总线。
    * 为单节点部署提供了 `NoOp` (空操作) 总线。
    * 消息去重机制，确保集群环境下消息的精确一次处理。
* **可观测性**:
    * 集成 Prometheus 指标，用于监控连接数、消息量、房间数和错误。
    * 使用 `log/slog` 进行结构化日志记录。
* **服务端 SDK**:
    * 提供便捷的 SDK，用于将 GoHub 功能集成到您的业务逻辑中。
    * 事件驱动：可订阅客户端连接、断开、消息和房间事件。
* **配置管理**:
    * 通过 Viper 使用 YAML 文件和环境变量进行灵活配置。
    * 支持配置热加载。
* **高并发处理**: 利用 Go 的并发特性高效处理大量并发连接。
* **健壮的错误处理**: 标准化的错误响应和错误码。
* **全面的测试**: 包括单元测试、集成测试、压力测试和分布式测试。

## 🚀 快速开始

### 环境要求

* Go 1.20 或更高版本
* (可选，用于集群模式) NATS Server (推荐 v2.8+，使用 JetStream 以支持持久化) 或 Redis。

### 安装

1.  **克隆仓库:**
    ```bash
    git clone [https://github.com/chenxilol/gohub.git](https://github.com/chenxilol/gohub.git) # 如果您的仓库 URL 不同，请替换
    cd gohub
    ```
2.  **下载依赖:**
    ```bash
    go mod tidy
    ```

### 配置

1.  **复制示例配置文件:**
    ```bash
    cp configs/config.example.yaml configs/config.yaml
    ```
2.  **编辑 `configs/config.yaml`:**
    * 对于无需外部依赖的快速**单节点启动**，请确保：
        ```yaml
        cluster:
          enabled: false
          bus_type: "noop" # 或者在集群禁用时移除 bus_type，默认为 noop
        ```
    * 对于**集群模式**，请启用它并配置您选择的消息总线 (NATS 或 Redis)。
    * 更新 `auth.secret_key` 用于 JWT 生成。

    *请参考 `configs/config.example.yaml` 查看所有可用选项。*

### 运行 GoHub

1.  **从源码运行:**
    ```bash
    go run cmd/gohub/main.go -config configs/config.yaml
    ```
    默认情况下，服务将在 `config.yaml` 中指定的地址启动 (例如：`:8080`)。WebSocket 端点位于 `/ws`。

2.  **使用 Docker (推荐用于集群模式):**
    提供了一个基础的 `docker-compose.yaml`。 为了更方便地搭建包含 NATS/Redis 等依赖的完整集群环境，建议您增强此 `docker-compose.yaml` 文件。
    ```bash
    # 示例：构建并运行其中一个定义的服务
    docker-compose build gohub1
    docker-compose up gohub1
    ```


## 消息格式

GoHub 使用基于 JSON 的消息格式：

```json
{
  "message_id": 123,          // 可选: 消息的唯一 ID (客户端生成，用于请求/响应关联)
  "message_type": "your_type", // 字符串: 消息类型 (例如："ping", "join_room", "custom_action")
  "data": { ... },            // 对象: 消息的有效负载，其结构取决于 message_type
  "request_id": 456         // 可选: 如果这是一条响应消息，它可能对应原始请求的 message_id
}
```


关于内置消息类型（如 `ping`, `join_room`, `leave_room`, `room_message`, `direct_message`）的示例，请参考 `internal/handlers/handlers.go`。

## 🛠️ API 端点

除了 `/ws` WebSocket 端点外，GoHub 还暴露了一些 HTTP API 端点：

* **`/metrics`**: Prometheus 指标，用于监控。
* **`/health`**: 健康检查端点。返回服务器状态和版本。
* **`/api/broadcast` (POST)**: 向所有连接的客户端广播消息。
  * 请求体: `{"message": "your message content"}`
* **`/api/stats` (GET)**: 获取服务器统计信息 (客户端数量、房间数量)。
* **`/api/rooms` (GET, POST)**:
  * GET: 列出所有房间及其成员数量。
  * POST: 创建一个新房间。请求体: `{"id": "room_id", "name": "Room Name", "max_clients": 100}`
* **`/api/clients` (GET)**: 获取客户端统计信息。

## 🏗️ 项目结构

GoHub 遵循标准的 Go 项目布局：

* **`cmd/gohub/`**: 主应用程序入口和服务器设置。
* **`configs/`**: 配置文件和加载逻辑。
* **`internal/`**: 核心内部包：
  * `auth/`: 认证 (JWT) 和授权逻辑。
  * `bus/`: 消息总线接口和实现 (NATS, Redis, NoOp)。
  * `dispatcher/`: 将传入的 WebSocket 消息路由到适当的处理器。
  * `handlers/`: WebSocket 消息的默认处理器。
  * `hub/`: 管理 WebSocket 客户端、房间和消息流。
  * `metrics/`: Prometheus 指标收集。
  * `sdk/`: 用于业务逻辑集成的服务端 SDK。
  * `utils/`: 通用工具函数。
  * `websocket/`: WebSocket 连接适配器接口和实现。
* **`scripts/`**: 工具和测试脚本。

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

* **单元测试**: 与代码一起位于 `_test.go` 文件中 (例如, `internal/bus/nats/nats_test.go`)。
* **集成测试**: 位于 `internal/integration/` 和 `cmd/gohub/` 的部分内容，用于服务器级测试。
* **压力测试**: `cmd/gohub/stress_test.go` 评估高负载下的性能。
* **分布式测试**: `cmd/gohub/redis_distributed_test.go` 专门测试使用 Redis 的集群功能。
* **Shell 脚本**: `scripts/` 目录包含用于运行各种测试的脚本。
  * `scripts/test_nats.sh`: 测试 NATS 功能。
  * `test_broadcast.sh` / `test_redis_broadcast.sh`: 端到端广播测试。

运行测试 (假设相关测试依赖如 NATS/Redis 可用):
```bash
go test ./...
# 或者使用 scripts/ 目录下的特定脚本，例如：
./scripts/test_bus.sh 
```

## 🤝 贡献

欢迎贡献！请随时提交 Pull Request 或开启 Issue。

在贡献之前，请：
1.  确保您的代码符合 Go 的最佳实践和项目现有风格。
2.  为您的更改编写或更新测试。
3.  使用 `go fmt` 或 `gofumpt` 格式化您的代码。
4.  考虑运行 `go vet` 和 `golangci-lint` (如果提供了配置文件)。

## 📜 许可证

GoHub 使用 MIT 许可证。详情请参阅仓库根目录下的 `LICENSE` 文件。
```