#!/bin/bash

# 测试NATS消息总线的脚本

# 确保脚本在错误时退出
set -e

# 检查NATS服务器是否已安装
if ! command -v nats-server &> /dev/null; then
    echo "Error: nats-server not found. Please install it first."
    echo "You can install it using: go install github.com/nats-io/nats-server/v2@latest"
    exit 1
fi

# 启动NATS服务器
echo "Starting NATS server..."
nats-server -p 4222 -m 8222 &
NATS_PID=$!

# 确保在脚本退出时关闭NATS服务器
trap 'echo "Stopping NATS server..."; kill $NATS_PID; wait $NATS_PID 2>/dev/null || true' EXIT

# 等待NATS服务器启动
sleep 1

# 运行测试
echo "Running NATS tests..."
cd "$(dirname "$0")/.."
go test -v ./internal/bus/nats/...

echo "All tests passed!" 