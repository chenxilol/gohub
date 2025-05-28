# GoHub - 高性能分布式 Go WebSocket 框架

[![Go 版本](https://img.shields.io/badge/go%20version-%3E%3D1.20-6F93CF.svg)](https://golang.org/dl/)
[![许可证: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**GoHub** 是一个基于Gorilla WebSocket开发的、强大的分布式WebSocket框架，专为高并发实时通信系统设计。其核心优势在于**完全分布式架构设计**和**高效的消息分发机制**，可轻松扩展至数十万并发连接，同时保持高吞吐量和低延迟。

## 🌟 核心特性

- **🚀 极简 API**: 几行代码即可启动功能完整的 WebSocket 服务器
- **🌐 分布式设计**: 内置集群支持，支持 NATS 和 Redis 作为消息总线
- **📨 灵活的消息处理**: 基于类型的消息路由和自定义处理器
- **🏠 房间管理**: 内置房间/频道功能，支持跨节点通信
- **🔐 认证授权**: JWT 认证和细粒度权限控制
- **📊 可观测性**: 集成 Prometheus 指标和结构化日志
- **⚡ 高性能**: 单节点支持 10万+ 并发连接

## 🚀 快速开始

### 安装

```bash
go get github.com/chenxilol/gohub
```

### 最简单的示例

```go
package main

import (
	"context"
	"log"
	"github.com/chenxilol/gohub/server"
)

func main() {
	// 创建服务器 - 就是这么简单！
	srv, err := server.NewServer(nil) // 使用默认配置
	if err != nil {
		log.Fatal(err)
	}

	// 启动服务器
	log.Println("Server starting on :8080...")
	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}
```

### 添加自定义消息处理器

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"github.com/chenxilol/gohub/server"
	"github.com/chenxilol/gohub/internal/hub"
)

func main() {
	// 创建服务器
	srv, err := server.NewServer(&server.Options{
		Address:        ":8080",
		AllowAnonymous: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	// 注册消息处理器
	srv.RegisterHandler("echo", func(ctx context.Context, client *hub.Client, data json.RawMessage) error {
		// 直接回显消息
		return client.Send(hub.Frame{
			MsgType: 1, // TextMessage
			Data:    data,
		})
	})

	// 启动服务器
	log.Fatal(srv.Start())
}
```

### 监听事件

```go
// 监听客户端连接事件
srv.On(sdk.EventClientConnected, func(ctx context.Context, event sdk.Event) error {
	log.Printf("Client connected: %s", event.ClientID)
	return nil
})

// 监听客户端断开事件
srv.On(sdk.EventClientDisconnected, func(ctx context.Context, event sdk.Event) error {
	log.Printf("Client disconnected: %s", event.ClientID)
	return nil
})
```

## 💻 更多示例

### 1. 带认证的服务器

```go
srv, err := server.NewServer(&server.Options{
	Address:        ":8080",
	EnableAuth:     true,
	AllowAnonymous: false,
	JWTSecretKey:   "your-secret-key",
	JWTIssuer:      "your-app-name",
})
```

### 2. 集群模式（使用 NATS）

```go
srv, err := server.NewServer(&server.Options{
	Address:       ":8080",
	EnableCluster: true,
	BusType:       "nats",
	NATSConfig: &nats.Config{
		URLs: []string{"nats://localhost:4222"},
		Name: "gohub-node-1",
	},
})
```

### 3. 集群模式（使用 Redis）

```go
srv, err := server.NewServer(&server.Options{
	Address:       ":8080",
	EnableCluster: true,
	BusType:       "redis",
	RedisConfig: &redis.Config{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	},
})
```

### 4. 添加自定义 HTTP 端点

```go
// 添加统计 API
srv.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"clients": srv.SDK().GetClientCount(),
		"rooms":   srv.SDK().GetRoomCount(),
	}
	json.NewEncoder(w).Encode(stats)
})

// 添加房间广播 API
srv.HandleFunc("/api/broadcast/room", func(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RoomID  string      `json:"room_id"`
		Message interface{} `json:"message"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	msgBytes, _ := json.Marshal(req.Message)
	srv.SDK().BroadcastToRoom(req.RoomID, msgBytes)
	
	w.WriteHeader(http.StatusOK)
})
```
```go

## 📊 与其他WebSocket框架对比

### 5. 使用 SDK 进行高级操作
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


// 获取 SDK 实例
sdk := srv.SDK()

// 向所有客户端广播
sdk.BroadcastAll([]byte(`{"type":"announcement","message":"Hello everyone!"}`))

// 向特定客户端发送消息
sdk.SendToClient(clientID, []byte(`{"type":"private","message":"Hello!"}`))

// 创建房间
sdk.CreateRoom("room-1", "Chat Room", 100)

// 让客户端加入房间
sdk.JoinRoom("room-1", clientID)

// 向房间广播
sdk.BroadcastToRoom("room-1", []byte(`{"type":"room","message":"Welcome!"}`))

// 获取房间成员
members, _ := sdk.GetRoomMembers("room-1")
```

## 📚 配置选项

```go
type Options struct {
	// HTTP服务器地址，默认 ":8080"
	Address string

	// 集群配置
	EnableCluster bool   // 是否启用集群模式，默认 false
	BusType      string // 消息总线类型: "nats", "redis", "noop"

	// 认证配置
	EnableAuth     bool   // 是否启用认证，默认 false
	AllowAnonymous bool   // 是否允许匿名连接，默认 true
	JWTSecretKey   string // JWT密钥
	JWTIssuer      string // JWT签发者

	// WebSocket配置
	ReadTimeout      time.Duration // 读超时，默认 60s
	WriteTimeout     time.Duration // 写超时，默认 60s
	ReadBufferSize   int          // 读缓冲区大小，默认 4KB
	WriteBufferSize  int          // 写缓冲区大小，默认 4KB

	// 日志级别: "debug", "info", "warn", "error"
	LogLevel string
}
```

## 🔌 客户端连接

### JavaScript 示例

```javascript
// 连接到服务器
const ws = new WebSocket('ws://localhost:8080/ws');

// 如果启用了认证
// const ws = new WebSocket('ws://localhost:8080/ws?token=your-jwt-token');

ws.onopen = () => {
	console.log('Connected to GoHub');
	
	// 发送消息
	ws.send(JSON.stringify({
		message_type: "echo",
		data: { text: "Hello, GoHub!" }
	}));
};

ws.onmessage = (event) => {
	const message = JSON.parse(event.data);
	console.log('Received:', message);
};
```

## 📊 内置端点

- `/ws` - WebSocket 连接端点
- `/health` - 健康检查
- `/metrics` - Prometheus 指标

## 🏗️ 架构设计

GoHub 采用模块化设计，主要组件包括：

- **Server**: 顶层封装，提供简单易用的 API
- **Hub**: 核心连接管理器
- **Dispatcher**: 消息分发器
- **MessageBus**: 集群通信总线（NATS/Redis）
- **SDK**: 服务端操作接口

## 📈 性能

- 单节点支持 10万+ 并发 WebSocket 连接
- 消息延迟 < 1ms（本地）
- 集群模式下支持水平扩展至百万级连接

## 🤝 贡献

欢迎贡献代码！请查看 [贡献指南](CONTRIBUTING.md)。

## 📄 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件

## 🔗 链接

- [完整文档](https://github.com/chenxilol/gohub/wiki)
- [示例代码](https://github.com/chenxilol/gohub/tree/main/examples)
- [问题反馈](https://github.com/chenxilol/gohub/issues)
