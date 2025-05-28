package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chenxilol/gohub/internal/hub"
	"github.com/chenxilol/gohub/internal/sdk"
	"github.com/chenxilol/gohub/server"
)

func main() {
	// 创建服务器，使用默认配置
	srv, err := server.NewServer(&server.Options{
		Address:        ":8080",
		AllowAnonymous: true,
		LogLevel:       "info",
	})
	if err != nil {
		log.Fatal("Failed to create server:", err)
	}

	// 注册自定义消息处理器
	_ = srv.RegisterHandler("echo", handleEcho)
	_ = srv.RegisterHandler("hello", handleHello)

	// 监听客户端连接/断开事件
	srv.On(sdk.EventClientConnected, func(ctx context.Context, event sdk.Event) error {
		log.Printf("Client connected: %s", event.ClientID)
		return nil
	})

	srv.On(sdk.EventClientDisconnected, func(ctx context.Context, event sdk.Event) error {
		log.Printf("Client disconnected: %s", event.ClientID)
		return nil
	})

	// 添加自定义HTTP端点
	srv.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		stats := map[string]interface{}{
			"clients": srv.SDK().GetClientCount(),
			"rooms":   srv.SDK().GetRoomCount(),
		}
		_ = json.NewEncoder(w).Encode(stats)
	})

	// 启动服务器
	go func() {
		log.Println("Starting server on :8080...")
		if err := srv.Start(); err != nil {
			log.Fatal("Server error:", err)
		}
	}()

	// 等待退出信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	// 优雅关闭
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Shutdown error:", err)
	}
}

// handleEcho 回显消息处理器
func handleEcho(_ context.Context, client *hub.Client, data json.RawMessage) error {
	// 解析请求
	var request map[string]interface{}
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}

	// 构造回复
	response := map[string]interface{}{
		"message_type": "echo_reply",
		"data":         request,
	}

	// 发送回复
	responseBytes, _ := json.Marshal(response)
	return client.Send(hub.Frame{
		MsgType: 1, // TextMessage
		Data:    responseBytes,
	})
}

// handleHello 问候消息处理器
func handleHello(_ context.Context, client *hub.Client, data json.RawMessage) error {
	var request struct {
		Name string `json:"name"`
	}

	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}

	response := map[string]interface{}{
		"message_type": "hello_reply",
		"data": map[string]string{
			"greeting": "Hello, " + request.Name + "!",
		},
	}

	responseBytes, _ := json.Marshal(response)
	return client.Send(hub.Frame{
		MsgType: 1, // TextMessage
		Data:    responseBytes,
	})
}
