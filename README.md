# GoHub - 强大的Go WebSocket服务

GoHub是一个基于Go语言开发的高性能WebSocket框架，专为实时通信设计。它提供了房间管理、认证授权、可靠的消息传递和分布式集群支持，适用于聊天应用、实时协作工具和在线游戏等场景。

## 特性

- **WebSocket通信**: 使用标准WebSocket协议，支持文本和二进制消息
- **房间/频道管理**: 创建、加入、离开房间，房间内广播
- **JWT认证与授权**: 提供基于JWT的认证和权限控制
- **消息分发**: 灵活的消息路由机制
- **集群支持**: 使用NATS作为消息总线，实现跨节点通信
- **指标监控**: 集成Prometheus指标
- **服务端SDK**: 简化业务逻辑与WebSocket功能的集成
- **标准错误处理**: 统一的错误响应格式
- **高并发**: 使用Go的并发特性，高效处理大量连接

## 快速开始

### 前提条件

- Go 1.20+
- (可选) NATS Server 2.8+ (用于集群模式)

### 安装

```bash
git clone https://github.com/yourusername/gohub.git
cd gohub
go mod download
```

### 配置

复制示例配置文件:

```bash
cp configs/config.example.yaml configs/config.yaml
```

编辑 `configs/config.yaml` 根据需要调整配置。

### 运行

```bash
go run cmd/gohub/main.go
```

服务将在 `http://localhost:8080` 启动，WebSocket端点位于 `ws://localhost:8080/ws`。

## 客户端连接

使用任何WebSocket客户端库连接:

```javascript
// 示例: 使用JavaScript
const ws = new WebSocket('ws://localhost:8080/ws?token=your-jwt-token');

ws.onopen = () => {
  console.log('Connected to GoHub');
  
  // 发送ping消息
  ws.send(JSON.stringify({
    message_id: 1,
    message_type: "ping",
    data: { ts: Date.now() }
  }));
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  console.log('Received:', message);
};
```

## API端点

- `/ws` - WebSocket连接端点
- `/metrics` - Prometheus指标 (用于监控)
- `/health` - 健康检查端点

## 认证

GoHub支持基于JWT的认证。可以通过以下方式提供令牌:
1. 查询参数: `ws://localhost:8080/ws?token=your-jwt-token`
2. Authorization头: `Authorization: Bearer your-jwt-token`

## 代码结构

- `cmd/gohub/` - 主应用程序入口
- `internal/` - 内部包
  - `auth/` - 认证与授权
  - `bus/` - 消息总线 (NATS实现)
  - `dispatcher/` - 消息分发
  - `handlers/` - 消息处理器
  - `hub/` - 连接和房间管理
  - `metrics/` - 监控指标
  - `sdk/` - 服务端SDK
  - `websocket/` - WebSocket适配层

## 贡献

欢迎贡献！请随时提交PR或Issue。

## 许可证

MIT 