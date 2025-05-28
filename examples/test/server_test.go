package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/chenxilol/gohub/internal/hub"
	"github.com/chenxilol/gohub/server"
	"github.com/gorilla/websocket"
)

func TestSimpleServer(t *testing.T) {
	// 创建服务器
	srv, err := server.NewServer(&server.Options{
		Address:        ":8081",
		AllowAnonymous: true,
		LogLevel:       "info", // 减少测试时的日志噪音
	})
	if err != nil {
		t.Fatal("Failed to create server:", err)
	}

	// 注册测试处理器
	srv.RegisterHandler("test", func(ctx context.Context, client *hub.Client, data json.RawMessage) error {
		// 发送回复
		response := map[string]interface{}{
			"message_type": "test_reply",
			"data":         string(data),
		}
		_ = response
		return nil
	})

	// 在后台启动服务器
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			t.Error("Server error:", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 创建WebSocket客户端连接
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial("ws://localhost:8081/ws", nil)
	if err != nil {
		t.Fatal("Failed to connect:", err)
	}
	defer conn.Close()

	// 发送测试消息
	testMsg := map[string]interface{}{
		"message_type": "test",
		"data":         "hello",
	}

	if err := conn.WriteJSON(testMsg); err != nil {
		t.Fatal("Failed to send message:", err)
	}

	// 读取响应
	var response map[string]interface{}
	if err := conn.ReadJSON(&response); err == nil {
		t.Logf("Received response: %v", response)
	}

	// 关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Error("Shutdown error:", err)
	}
}

func TestServerWithAuth(t *testing.T) {
	// 创建带认证的服务器
	srv, err := server.NewServer(&server.Options{
		Address:        ":8082",
		EnableAuth:     true,
		AllowAnonymous: false,
		JWTSecretKey:   "test-secret-key",
		JWTIssuer:      "test-issuer",
		LogLevel:       "error",
	})
	if err != nil {
		t.Fatal("Failed to create server:", err)
	}

	// 启动服务器
	go func() {
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			t.Error("Server error:", err)
		}
	}()

	// 等待服务器启动
	time.Sleep(100 * time.Millisecond)

	// 尝试不带token连接（应该失败）
	dialer := websocket.Dialer{}
	_, resp, err := dialer.Dial("ws://localhost:8082/ws", nil)
	if err == nil {
		t.Error("Expected connection to fail without token")
	} else if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 status, got %d", resp.StatusCode)
	}

	// 关闭服务器
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Error("Shutdown error:", err)
	}
}
