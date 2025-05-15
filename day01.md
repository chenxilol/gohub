# gohub Phase‑1 Implementation Spec

> **Audience**: Large‑language–model code assistants (e.g. ChatGPT‑Code, Copilot‑Chat) and human contributors.\*\*受众 \*\*：大型语言模型代码助手（例如 ChatGPT-Code、Copilot-Chat）和人类贡献者。   &#x20;
> **Objective**: Generate compile‑ready Go code that delivers the *minimal viable hub* (MVH): single‑node WebSocket hub **plus** pluggable etcd message bus ready for cluster mode.**目标 **************：生成可编译的 Go 代码，提供\*最小可行集线器 \* （MVH）：单节点 WebSocket 集线器**************加上**可用于集群模式的可插拔 etcd 消息总线。

---

## 0 Scope0 范围 ✅

Phase‑1 focuses on the **backend only**; no React demo yet.第一阶段\*\*只关注后端 \*\*;还没有 React 演示。

* 📌 *Single‑node* features MUST work **stand‑alone** without running etcd. ✅ 
* 📌 When `cluster.enabled = true` the same binary MUST switch to **multi‑node** mode, using etcd *watch/put* for broadcast & unicast. ✅ 
* 📌 Public API surface **MUST remain stable** for future phases. ✅ 

## 1 Deliverables & Dir Layout1 可交付成果和目录布局 ✅

```
internal/
  hub/           hub.go client.go message.go frame.go ✅ 
  bus/           bus.go etcd_bus.go ✅ 
  websocket/     adapter.go cfg.go conn_iface.go ✅ 
  dispatcher/    dispatcher.go ✅ 
  handlers/      ping.go ✅ 
configs/
  config.yaml    # default ✅ 
cmd/gohub/
  main.go ✅ 
```

`go test ./...` MUST succeed (≥80 % coverage). An example **docker-compose.yaml** (Go + etcd) MUST be generated. ✅ 

## 2 Config (YAML)2 配置 （YAML） ✅ 

```yaml
server:
  addr: ":8080"
  read_timeout: 3m
  write_timeout: 3m
cluster:
  enabled: false         # true => etcd bus
  etcd:
    endpoints: ["http://etcd:2379"]
    dial_timeout: 5s
    key_prefix: "/gohub"
```

Parsing via **spf13/viper**. ✅ 

## 3 Core Interfaces ✅ 

### 3.1 WSConn *(internal/websocket/conn\_iface.go)* ✅ 

```go
// WSConn decouples Gorilla / nhooyr for testability.
type WSConn interface {
    ReadMessage() (int, []byte, error)
    WriteMessage(int, []byte) error
    WriteControl(int, []byte, time.Time) error
    SetReadLimit(int64)
    SetReadDeadline(time.Time) error
    SetWriteDeadline(time.Time) error
    Close() error
}
```

### 3.2 MessageBus *(internal/bus/bus.go)* ✅ 

```go
// MessageBus propagates frames between nodes.
type MessageBus interface {
    Publish(ctx context.Context, topic string, data []byte) error
    Subscribe(ctx context.Context, topic string) (<-chan []byte, error)
    Unsubscribe(topic string) error
    Close() error
}
```

Topics: `broadcast`, `unicast/<clientID>`. ✅ 

### 3.3 Hub Public Surface ✅ 

```go
// NewHub(bus MessageBus, cfg hub.Config) *Hub
// (*Hub) Register(c *Client)
// (*Hub) Push(id string, f Frame) error
// (*Hub) Broadcast(f Frame)
// (*Hub) GetClientCount() int
```

## 4 Data Types ✅ 

```go
// Frame wraps low‑level websocket frame.
struct Frame {
    MsgType int           // websocket.TextMessage / Binary
    Data    []byte
    Ack     chan error    // nil => fire‑and‑forget
}
```

```go
// Message = application‑level payload.
type Message struct {
    ID   int             `json:"message_id"`
    Type string          `json:"message_type"`
    Data json.RawMessage `json:"data,omitempty"`
    Ts   int64           `json:"ts,omitempty"`
}
```

## 5 EtcdBus Algorithm (Minimal) ✅ 

```text
Put  : key = <prefix>/broadcast,        val = <Frame bytes>
Unicast : <prefix>/unicast/<clientID>
Watch: WithPrefix, rev = 0, independent goroutine
Ack: not required phase‑1 (at‑least‑once) – handler side may dedup via crc32.
Lease TTL = 30s, auto‑expire.
```

## 6 Concurrency Rules ✅ 

| Component          | Goroutine(s)                                                                 | Notes                                                |
| ------------------ | ---------------------------------------------------------------------------- | ---------------------------------------------------- |
| \*\*Client  \*\*   | readLoop, writeLoop                                                          | buffered channel `out` size = cfg.MessageBufferCap   |
| \*\*Hub  \*\*      | read‑only on `sync.Map`; writes behind mutex only when register/unregister   |                                                      |
| \*\*EtcdBus  \*\*  | 1 watch loop → deliver to Hub; publish is synchronous                        |                                                      |

## 7 Error Handling Contract ✅ 

* **Hub.Push** returns `ErrClientNotFound` (exported sentinel). ✅ 
* **Send buffer full** ⇒ `ErrSendBufferFull`, caller decides. ✅ 
* **Slow client** (write timeout) ⇒ Client shutdown. ✅ 
* Bus failure fallback: if `Publish` errors, Hub logs **warn** and still delivers locally. ✅ 

## 8 Testing Matrix

| Case   | Mode                          | Expected                        |
| ------ | ----------------------------- | ------------------------------- |
| T1     | Single node broadcast         | All local clients receive       |
| T2     | Cluster broadcast (2 nodes)   | Both nodes' clients receive     |
| T3     | Unicast across nodes          | Only target receives            |
| T4     | Heartbeat timeout             | Client disconnected & removed   |
| T5     | Send buffer full              | ErrSendBufferFull returned      |

Use **etcd‑embed** for cluster tests; random port, cleanup in `t.Cleanup()`.   &#x20;

## 9 Coding Guidelines (for AI) ✅ 

1. **Package comments** — every package must start with a 1‑sentence overview in English. ✅ 
2. **No global state** except the singleton dispatcher (`var d = NewDispatcher()`). All other dependencies must be passed explicitly or injected via factories. ✅ 
3. **Use ****\`\`**** as the first parameter** for every I/O‑like function (network, disk, bus). Honor `ctx.Done()` for cancellation. ✅ 
4. **Naming conventions** — follow the official *Effective Go* rules: ✅ 

   * Exported identifiers **PascalCase**; unexported **camelCase**. ✅ 
   * Common acronyms stay **all‑caps** (e.g. `ID`, `HTTP`, `JSON`). Write `UserID` not `UserId`. ✅ 
   * **Package names** are all‑lowercase, no underscores, no plurals (`dispatcher`, not `dispatchers`). ✅ 
   * **File names** are `snake_case.go` or all‑lowercase if short (`hub.go`, `client_io.go`). ✅ 
   * Error sentinel variables use the `ErrSomething` pattern; avoid inline errors where reuse is expected. ✅ 
   * Interface names are noun‑phrases and, when appropriate, end with `‑er` (`MessageBus`, `Closer`). ✅ 
5. **Formatting & lint** — run `goimports` and ensure `golangci‑lint run` reports *zero* issues. ✅ 
6. **Test/Benchmark naming** — `Test<Component>_<Scenario>`, `Benchmark_<Func>`. ✅ 
