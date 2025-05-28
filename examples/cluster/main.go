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

	hubnats "github.com/chenxilol/gohub/internal/bus/nats"
	hubredis "github.com/chenxilol/gohub/internal/bus/redis"
	"github.com/chenxilol/gohub/server"
)

func main() {
	// 示例1: 使用NATS作为消息总线的集群模式
	natsExample()

	// 示例2: 使用Redis作为消息总线的集群模式
	// redisExample()
}

// natsExample 演示使用NATS的集群模式
func natsExample() {
	srv, err := server.NewServer(&server.Options{
		Address:       ":8080",
		EnableCluster: true,
		BusType:       "nats",
		NATSConfig: &hubnats.Config{
			URLs:          []string{"nats://localhost:4222"},
			Name:          "gohub-node-1",
			ReconnectWait: 2 * time.Second,
			MaxReconnects: -1,
		},
		AllowAnonymous: true,
		LogLevel:       "info",
	})
	if err != nil {
		log.Fatal("Failed to create server:", err)
	}

	runServer(srv)
}

// redisExample 演示使用Redis的集群模式
func redisExample() {
	srv, err := server.NewServer(&server.Options{
		Address:       ":8080",
		EnableCluster: true,
		BusType:       "redis",
		RedisConfig: &hubredis.Config{
			Addrs:    []string{"localhost:6379"},
			Password: "",
			DB:       0,
			PoolSize: 10,
			MinConn:  5,
		},
		AllowAnonymous: true,
		LogLevel:       "info",
	})
	if err != nil {
		log.Fatal("Failed to create server:", err)
	}

	runServer(srv)
}

// runServer 运行服务器的通用逻辑
func runServer(srv *server.Server) {
	// 添加房间广播API
	srv.HandleFunc("/api/broadcast/room", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			RoomID  string      `json:"room_id"`
			Message interface{} `json:"message"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}

		// 向房间广播消息
		msgBytes, _ := json.Marshal(req.Message)
		err := srv.SDK().BroadcastToRoom(req.RoomID, msgBytes, "")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// 启动服务器
	go func() {
		log.Println("Starting cluster server...")
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
