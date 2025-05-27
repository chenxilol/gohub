# GoHub - 高性能分布式 Go WebSocket 框架

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

## 🚀 快速开始 (作为库使用)

GoHub 设计为一个核心库，方便您集成到自己的 Go 应用程序中，以构建强大的 WebSocket 服务。

### 1. 安装 GoHub 库

在您的 Go 项目模块中，使用以下命令获取 GoHub：
```bash
go get github.com/chenxilol/gohub
```

### 2. 基本用法：构建您自己的 WebSocket 服务器

以下是一个简约示例，展示如何在您的应用程序中初始化 GoHub 组件、注册自定义消息处理器，并启动一个 WebSocket 服务器实例。

**推荐的配置方式是加载 YAML 文件：**

1.  **获取配置文件**: 从 GoHub 仓库的 `configs/` 目录下复制 `config.example.yaml` 到您的项目，例如将其命名为 `my-gohub-config.yaml`。
2.  **修改配置**: 根据您的需求编辑 `my-gohub-config.yaml`。对于最简单的单节点启动，您可以参考 `config.example.yaml` 中关于 `cluster.enabled: false` 和 `bus_type: "noop"` 的设置。
3.  **在代码中加载**: 使用 GoHub 提供的 `configs.LoadConfig()` 函数加载您的配置文件。

```go
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	"flag" // 用于允许通过命令行覆盖配置文件路径

	// 假设 GoHub 将其公共 API 组织在类似 'pkg' 的目录下或直接在顶层包。
	// 实际路径请参考 GoHub 的官方文档和源码结构。
	"github.com/chenxilol/gohub/pkg/configs"
	"github.com/chenxilol/gohub/pkg/dispatcher"
	"github.com/chenxilol/gohub/pkg/hub"
	"github.com/chenxilol/gohub/pkg/logging" // 假设有日志初始化包
	"github.com/chenxilol/gohub/pkg/server"   // 假设 NewGoHubServer 在此
	"github.com/chenxilol/gohub/pkg/websocket"
)

// handleMyCustomAction 示例自定义消息处理器
func handleMyCustomAction(ctx context.Context, client *hub.Client, data json.RawMessage) error {
	slog.Info("处理 'my_custom_action'", "client_id", client.ID(), "payload", string(data))

	var requestPayload struct {
		ActionDetail string `json:"action_detail"`
	}
	if err := json.Unmarshal(data, &requestPayload); err != nil {
		slog.Error("解析自定义操作负载失败", "error", err, "client_id", client.ID())
		errorResp := hub.NewError(hub.ErrCodeInvalidFormat, "my_custom_action 的负载无效", 0, err.Error())
		errorMsgJson, _ := errorResp.ToJSON()
		_ = client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: errorMsgJson})
		return err
	}

	slog.Info("自定义操作详情", "client_id", client.ID(), "detail", requestPayload.ActionDetail)

	replyData := map[string]interface{}{"status": "success", "action_processed": "my_custom_action"}
	responseHubMsg, err := hub.NewMessage(0, "my_custom_action_reply", replyData)
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

func main() {
	// 0. (可选) 允许通过命令行参数指定配置文件路径
	configFile := flag.String("config", "my-gohub-config.yaml", "Path to your GoHub configuration file")
	flag.Parse()

	// 1. 初始化日志 (推荐做法)
	// 您可以根据需要，在加载配置后根据配置文件的日志级别调整日志处理器
	handler := logging.NewSlogTextHandler(os.Stdout, &logging.SlogHandlerOptions{Level: slog.LevelInfo}) // 默认Info级别
	logging.SetupDefaultSlog(handler)


	// 2. 加载 GoHub 服务器配置从 YAML 文件
	// 确保您已将 GoHub 项目中的 'configs/config.example.yaml' 复制到您的项目
	// (例如，作为 'my-gohub-config.yaml') 并进行了相应修改。
	slog.Info("正在加载配置文件...", "path", *configFile)
	cfg, err := configs.LoadConfig(*configFile) // 确保 configs.LoadConfig 是公开的 API
	if err != nil {
		slog.Error("加载配置文件失败", "path", *configFile, "error", err)
		os.Exit(1)
	}
	// 您还可以根据 cfg.Log 中的设置，重新配置 slog 处理程序（例如，设置正确的日志级别和格式）
	// logging.SetupDefaultSlog(...) // 重新配置日志（如果需要）

	// (可选) 以编程方式覆盖或补充配置
	// cfg.HTTP.Address = ":9090" // 例如，覆盖HTTP监听地址
	// cfg.Auth.SecretKey = os.Getenv("GOHUB_SECRET_KEY") // 例如，从环境变量加载密钥

	slog.Info("配置加载成功。正在准备启动 GoHub 服务器...", "http_address", cfg.HTTP.Address)

	// 3. 获取全局分发器实例并注册您的消息处理器
	disp := dispatcher.GetDispatcher() // 假设 GetDispatcher() 是公开的 API
	disp.Register("my_custom_action", handleMyCustomAction)
	slog.Info("已成功注册自定义消息处理器 'my_custom_action'")

	// 4. 创建 GoHub 服务器实例
	gohubServer, err := server.NewGoHubServer(cfg) // 假设 NewGoHubServer 接受 *configs.Config
	if err != nil {
		slog.Error("创建 GoHub 服务器实例失败", "error", err)
		os.Exit(1)
	}

	// 5. 启动服务器 (在 goroutine 中以非阻塞方式启动)
	go func() {
		slog.Info("GoHub 服务器正在启动...", "address", cfg.HTTP.Address)
		if err := gohubServer.Start(); err != nil { // 假设 Start() 方法存在
			slog.Error("GoHub 服务器启动失败", "error", err)
			os.Exit(1)
		}
	}()
	slog.Info("GoHub 服务器已在 "+cfg.HTTP.Address+" 监听", "url", "ws://localhost"+cfg.HTTP.Address+"/ws")

	// 6. 实现优雅关闭
	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)
	
	receivedSignal := <-quitChannel
	slog.Info("接收到关闭信号", "signal", receivedSignal.String())

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	slog.Info("正在优雅地关闭 GoHub 服务器...")
	if err := gohubServer.Shutdown(shutdownCtx); err != nil { // 假设 Shutdown(context.Context) 方法存在
		slog.Error("服务器关闭失败", "error", err)
	} else {
		slog.Info("服务器已成功关闭")
	}
}
```
*上面的代码是一个指导性示例。您需要根据 GoHub 库的实际公共 API（包结构、函数签名、配置结构等）进行调整。确保 `configs.LoadConfig` 和 `server.NewGoHubServer` 等函数是您库导出的公共API。*

## ⚙️ 运行 GoHub 自带的示例服务器 (可选)

如果您想快速运行 GoHub 项目中包含的一个完整功能的示例服务器（例如，用于测试或查看其运行方式），可以按以下步骤操作：

### 环境要求与安装 (针对运行示例)

*   Go 1.20 或更高版本
*   Git

1.  **克隆 GoHub 仓库:**
    ```bash
    git clone https://github.com/chenxilol/gohub.git
    cd gohub
    ```

2.  **下载依赖:**
    ```bash
    go mod tidy
    ```

### 配置示例服务器

1.  **复制示例配置文件:**
    ```bash
    cp configs/config.example.yaml configs/config.yaml
    ```

2.  **编辑 `configs/config.yaml`:**
    *   对于快速**单节点启动** (无需外部依赖):
        ```yaml
        cluster:
          enabled: false
          bus_type: "noop"
        # http:
        #   address: ":8080" # 默认监听地址和端口
        ```
    *   确保更新 `auth.secret_key`，例如使用 `openssl rand -hex 32` 生成。一个安全的密钥对于生产环境至关重要。
    *   如需体验**分布式集群模式** (推荐用于生产环境)，您需要启用 `cluster` (设置 `enabled: true`) 并配置消息总线 (NATS 或 Redis)。请参考 `configs/config.example.yaml` 中关于 `bus_type` (设置为 `"nats"` 或 `"redis"`) 的部分，并配置相应的 NATS 或 Redis 服务器地址。

### 运行示例服务器

1.  **从源码运行 (推荐用于快速尝试):**
    ```bash
    go run cmd/gohub/main.go -config configs/config.yaml
    ```
    服务启动后，您可以通过 WebSocket 客户端连接到 `ws://localhost:8080/ws` (假设您使用了默认端口配置)。

2.  **使用 Docker (适用于生产或模拟分布式环境):**
    ```bash
    # 启动完整的分布式环境 (通常 docker-compose.yml 预配置为使用 NATS)
    docker-compose up -d
    ```
    这将根据 `docker-compose.yml` 文件启动 GoHub 服务以及其依赖 (例如 NATS 消息总线)。

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

您可以为特定于应用程序的 WebSocket 消息添加自定义处理器。GoHub 提供了一个中心的、可获取的分发器实例。要在您的应用程序中添加自定义 WebSocket 消息处理器，您可以在初始化 GoHub 相关服务 (如通过 `server.NewGoHubServer`) *之前*，获取分发器实例并注册您的处理器。

以下是如何在您自己项目的 `main.go` (或相关的应用初始化代码) 中实现这一点：

**示例 (在您的应用程序的 `main.go` 或设置代码中):**

```go
// main.go (您的应用程序入口)
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
	"flag" // 用于示例中的配置文件加载

	// 假设的公共 API 路径。请根据 GoHub 的实际导出结构调整。
	"github.com/chenxilol/gohub/pkg/configs"
	"github.com/chenxilol/gohub/pkg/dispatcher"
	"github.com/chenxilol/gohub/pkg/hub"
	"github.com/chenxilol/gohub/pkg/logging" // 假设的日志初始化包
	"github.com/chenxilol/gohub/pkg/server"   // 假设 NewGoHubServer 和其他服务器相关功能在此
	"github.com/chenxilol/gohub/pkg/websocket"
)

// 1. 定义您的自定义处理函数
//    它应该匹配 dispatcher.HandlerFunc 签名
func handleMyCustomAction(ctx context.Context, client *hub.Client, data json.RawMessage) error {
    slog.Info("正在处理 'my_custom_action'", "client_id", client.ID(), "payload", string(data))
    
    var requestPayload struct {
        ActionDetail string `json:"action_detail"`
    }
    if err := json.Unmarshal(data, &requestPayload); err != nil {
        slog.Error("解析自定义操作负载失败", "error", err, "client_id", client.ID())
        errorResp := hub.NewError(hub.ErrCodeInvalidFormat, "my_custom_action 的负载无效", 0, err.Error())
        errorMsgJson, _ := errorResp.ToJSON()
        _ = client.Send(hub.Frame{MsgType: websocket.TextMessage, Data: errorMsgJson})
        return err 
    }

    slog.Info("自定义操作详情", "client_id", client.ID(), "detail", requestPayload.ActionDetail)

    // ... 您的业务逻辑代码 ...

    replyData := map[string]interface{}{"status": "success", "action_processed": "my_custom_action", "detail_received": requestPayload.ActionDetail}
    responseHubMsg, err := hub.NewMessage(0, "my_custom_action_reply", replyData)
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

func main() {
    // 示例：允许通过命令行标志指定配置文件路径
    configFile := flag.String("config", "configs/config.yaml", "Path to the configuration file")
    flag.Parse()

    // 初始化日志 (推荐做法)
	handler := logging.NewSlogTextHandler(os.Stdout, &logging.SlogHandlerOptions{Level: slog.LevelInfo})
	logging.SetupDefaultSlog(handler)

    // 加载配置
	cfg, err := configs.LoadConfig(*configFile) // 假设 LoadConfig 是公开的 API
	if err != nil {
		slog.Error("加载配置文件失败", "path", *configFile, "error", err)
		os.Exit(1)
	}
	// 您可能还想根据 cfg.Log 设置更具体的日志级别或格式

    // 2. 获取全局分发器实例
    d := dispatcher.GetDispatcher() // 假设 GetDispatcher 是公开的 API

    // 3. 注册您的自定义处理器
    d.Register("my_custom_action", handleMyCustomAction)
    slog.Info("已注册自定义消息处理器 'my_custom_action'")

    // 初始化并启动 GoHub 服务器
    // 假设 server.NewGoHubServer 是公开的 API
    gohubServer, err := server.NewGoHubServer(cfg) 
    if err != nil {
        slog.Error("创建 GoHub 服务器失败", "error", err)
        os.Exit(1)
    }
    
    go func() {
        slog.Info("GoHub 服务器正在启动...", "address", cfg.HTTP.Address)
		if err := gohubServer.Start(); err != nil { // 假设 Start() 方法存在
			slog.Error("GoHub 服务器启动失败", "error", err)
			os.Exit(1)
		}
	}()
    slog.Info("GoHub 服务器已在 "+cfg.HTTP.Address+" 监听", "url", "ws://localhost"+cfg.HTTP.Address+"/ws")


	// 实现优雅关闭
    quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)
	
	receivedSignal := <-quitChannel
	slog.Info("接收到关闭信号", "signal", receivedSignal.String())

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	slog.Info("正在优雅地关闭 GoHub 服务器...")
	if err := gohubServer.Shutdown(shutdownCtx); err != nil { // 假设 Shutdown(context.Context) 方法存在
		slog.Error("服务器关闭失败", "error", err)
	} else {
		slog.Info("服务器已成功关闭")
	}
}
```
*未来版本的 GoHub SDK 可能会提供更直接的处理器注册方法或更封装的服务器构建方式。请关注 GoHub 的更新日志和官方文档。*

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


### 1. 如何实现跨节点的消息传递？

GoHub使用消息总线（NATS或Redis）在集群节点间传递消息。当一个节点需要向连接到其他节点的客户端发送消息时，它会通过消息总线广播这个消息。其他节点会接收到这个消息，并将其转发给相应的客户端。整个过程对开发者透明，您只需正常使用API，不需要关心客户端连接在哪个节点上。

### 2. GoHub的分布式架构如何提高系统可靠性？

GoHub的分布式设计带来几个关键优势：
- **高可用性**：单个节点故障不会影响整个系统
- **水平扩展**：可以通过添加更多节点来增加系统容量
- **负载均衡**：连接可以分散到多个节点上
- **地理分布**：节点可以部署在不同区域，减少延迟

### 3. 如何监控集群状态？

GoHub通过Prometheus指标提供全面的监控能力，包括每个节点的连接数、消息处理量、房间数量等。您可以使用Grafana创建仪表板来可视化这些指标，实时监控集群健康状况。
