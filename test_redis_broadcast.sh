#!/bin/bash

# 停止可能存在的旧进程
pkill -f "go run cmd/gohub/main.go" 2>/dev/null || true
pkill -f "./ws_client" 2>/dev/null || true

# 清屏
clear

# 显示标题
echo "===== GoHub Redis 分布式 WebSocket 广播测试 ====="
echo "初始化测试环境..."


echo "Redis 服务器运行正常。"

# 编译测试客户端
echo "编译WebSocket测试客户端..."
go build -o ws_client test_client.go

# 在后台启动两个GoHub实例，使用 Redis 配置
echo "启动GoHub实例1 (端口8080) - 使用 Redis"
go run cmd/gohub/main.go -config configs/config-redis.yaml -port 8080 > /tmp/gohub1_redis.log 2>&1 &
PID1=$!

echo "启动GoHub实例2 (端口8081) - 使用 Redis"
go run cmd/gohub/main.go -config configs/config-redis.yaml -port 8081 > /tmp/gohub2_redis.log 2>&1 &
PID2=$!

# 等待服务启动
echo "等待服务启动..."
sleep 3

# 创建日志目录
mkdir -p logs

# 启动6个测试客户端
CLIENT_PIDS=()

echo "启动6个WebSocket客户端："
echo "1. 客户端A1 - 连接到实例1 (8080)"
./ws_client -port 8080 -id "客户端A1" > logs/redis_client_A1.log 2>&1 &
CLIENT_PIDS+=($!)

echo "2. 客户端A2 - 连接到实例1 (8080)"
./ws_client -port 8080 -id "客户端A2" > logs/redis_client_A2.log 2>&1 &
CLIENT_PIDS+=($!)

echo "3. 客户端B1 - 连接到实例2 (8081)"
./ws_client -port 8081 -id "客户端B1" > logs/redis_client_B1.log 2>&1 &
CLIENT_PIDS+=($!)

echo "4. 客户端B2 - 连接到实例2 (8081)"
./ws_client -port 8081 -id "客户端B2" > logs/redis_client_B2.log 2>&1 &
CLIENT_PIDS+=($!)

echo "5. 共享ID-1 - 连接到实例1 (8080)"
./ws_client -port 8080 -id "共享ID" > logs/redis_shared_1.log 2>&1 &
CLIENT_PIDS+=($!)

echo "6. 共享ID-2 - 连接到实例2 (8081)"
./ws_client -port 8081 -id "共享ID" > logs/redis_shared_2.log 2>&1 &
CLIENT_PIDS+=($!)

# 等待客户端连接
echo "等待客户端连接..."
sleep 2

echo ""
echo "测试环境已准备就绪！"
echo "所有服务和客户端都在后台运行，日志将被保存到logs/目录"
echo "使用 Redis 作为消息总线"
echo ""

# 显示客户端状态函数
show_client_status() {
    echo "正在检查所有客户端状态..."
    echo "----------------------------------------------"
    echo "客户端A1 (8080): 实例1"
    grep -A 1 "收到消息" logs/redis_client_A1.log | tail -2
    echo ""
    
    echo "客户端A2 (8080): 实例1"
    grep -A 1 "收到消息" logs/redis_client_A2.log | tail -2
    echo ""
    
    echo "客户端B1 (8081): 实例2"
    grep -A 1 "收到消息" logs/redis_client_B1.log | tail -2
    echo ""
    
    echo "客户端B2 (8081): 实例2"
    grep -A 1 "收到消息" logs/redis_client_B2.log | tail -2
    echo ""
    
    echo "共享ID-1 (8080): 实例1"
    grep -A 1 "收到消息" logs/redis_shared_1.log | tail -2
    echo ""
    
    echo "共享ID-2 (8081): 实例2"
    grep -A 1 "收到消息" logs/redis_shared_2.log | tail -2
    echo "----------------------------------------------"
}

# 等待用户准备好
read -p "按Enter键开始测试..." 

# 发送测试1 - 从实例1广播
echo ""
echo "测试1: 发送广播消息到实例1 (8080) - 使用 Redis"
curl -X POST -H "Content-Type: application/json" \
  -d '{"message":"这是通过 Redis 从实例1发出的广播消息 - 所有客户端都应该收到"}' \
  http://localhost:8080/api/broadcast
echo ""

# 给客户端一些时间接收消息
sleep 2
show_client_status

# 发送测试2 - 从实例2广播
echo ""
echo "测试2: 发送广播消息到实例2 (8081) - 使用 Redis"
curl -X POST -H "Content-Type: application/json" \
  -d '{"message":"这是通过 Redis 从实例2发出的广播消息 - 所有客户端都应该收到"}' \
  http://localhost:8081/api/broadcast
echo ""

# 给客户端一些时间接收消息
sleep 2
show_client_status

# 测试3 - 发送一条包含随机数的消息，便于区分
RANDOM_NUM=$RANDOM
echo ""
echo "测试3: 发送带随机数 $RANDOM_NUM 的广播消息到实例1 (8080) - 使用 Redis"
curl -X POST -H "Content-Type: application/json" \
  -d "{\"message\":\"Redis测试消息-$RANDOM_NUM - 所有客户端都应收到此唯一标识符\"}" \
  http://localhost:8080/api/broadcast
echo ""

# 给客户端一些时间接收消息
sleep 2
show_client_status

# Redis 特定测试 - 检查 Redis 键
echo ""
echo "Redis 键检查："
echo "Redis 中当前的键:"
redis-cli keys "gohub:*" | head -10

# 总结
echo ""
echo "===== 测试总结 ====="
echo "如果所有6个客户端都显示收到了相同的3条消息，说明基于 Redis 的分布式广播功能正常工作！"
echo "这证明 Redis 消息总线成功地将广播消息从一个节点传递到另一个节点。"
echo ""
echo "特别注意共享ID的客户端 - 这测试了多个客户端以相同ID登录的情况。"
echo ""

# 清理
read -p "测试完成。按Enter键清理测试环境..." 

echo "停止所有测试进程..."
kill $PID1 $PID2 ${CLIENT_PIDS[@]} 2>/dev/null || true

echo "清理临时文件..."
rm -f ws_client

echo "清理 Redis 测试键..."
redis-cli eval "return redis.call('del', unpack(redis.call('keys', 'gohub:*')))" 0

echo "测试环境已清理完毕。"
echo "日志文件保留在logs/目录中，您可以稍后查看。" 