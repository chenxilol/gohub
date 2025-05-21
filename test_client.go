package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	// 定义命令行参数
	port := flag.Int("port", 8080, "WebSocket服务端口")
	id := flag.String("id", "client", "客户端ID，用于在输出中区分")
	flag.Parse()

	// 构建WebSocket URL
	u := url.URL{Scheme: "ws", Host: fmt.Sprintf("localhost:%d", *port), Path: "/ws"}

	// 设置日志前缀
	log.SetPrefix(fmt.Sprintf("[%s] ", *id))
	log.SetFlags(0)

	// 打印连接信息
	log.Printf("正在连接到 %s", u.String())

	// 连接到WebSocket服务
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("连接失败:", err)
	}
	defer c.Close()

	// 处理中断信号
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// 接收消息的通道
	done := make(chan struct{})

	// 开始接收消息的goroutine
	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Println("读取失败:", err)
				return
			}
			// 打印收到的消息（使用明显的格式）
			fmt.Printf("\n=================================\n")
			fmt.Printf("客户端[%s] 收到消息: %s\n", *id, message)
			fmt.Printf("=================================\n\n")
		}
	}()

	// 保持连接活动状态
	for {
		select {
		case <-done:
			return
		case <-interrupt:
			log.Println("收到中断信号，关闭连接...")

			// 关闭WebSocket连接
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("写入关闭消息错误:", err)
				return
			}

			// 等待服务器关闭连接
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
