#!/bin/bash
set -e

# 颜色和格式
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# 项目根目录
ROOT_DIR=$(pwd)

# 标题输出函数
function print_header() {
    echo -e "\n${BOLD}${YELLOW}=> $1${NC}\n"
}

# 运行测试的函数
function run_test() {
    local test_package=$1
    local test_filter=$2
    local coverage_file=$3
    local timeout=${4:-"3m"}  # 默认超时时间3分钟

    if [ -n "$test_filter" ]; then
        print_header "运行测试: $test_package ($test_filter)"
        go test -v -race -timeout=$timeout -coverprofile=$coverage_file -run $test_filter $test_package
    else
        print_header "运行测试: $test_package"
        go test -v -race -timeout=$timeout -coverprofile=$coverage_file $test_package
    fi
}

# 创建临时目录用于存放覆盖率文件
COVERAGE_DIR=$(mktemp -d)
echo "覆盖率文件目录: $COVERAGE_DIR"

# 检查是否安装了nats-server
if ! command -v nats-server &> /dev/null; then
    echo -e "${RED}警告: 未找到nats-server, 测试可能会跳过${NC}"
    echo "请通过以下命令安装nats-server:"
    echo "  brew install nats-server  # MacOS"
    echo "  或参考 https://docs.nats.io/running-a-nats-service/introduction/installation"
    echo ""
    echo "是否继续测试? (y/n)"
    read -r continue_test
    if [ "$continue_test" != "y" ]; then
        exit 1
    fi
fi

# 启动NATS服务器（带JetStream支持）
NATS_PORT=4222
NATS_PID=""

function start_nats_server() {
    print_header "启动NATS服务器（端口: $NATS_PORT，启用JetStream）"
    nats-server -p $NATS_PORT -js &
    NATS_PID=$!
    echo "NATS服务器已启动，PID: $NATS_PID"
    # 等待服务器完全启动
    sleep 2
}

function stop_nats_server() {
    if [ -n "$NATS_PID" ]; then
        print_header "停止NATS服务器"
        kill $NATS_PID
        wait $NATS_PID 2>/dev/null || true
        echo "NATS服务器已停止"
    fi
}

# 设置测试环境变量，让测试使用我们启动的NATS服务器
export NATS_TEST_URL="nats://localhost:$NATS_PORT"

# 确保脚本结束时停止NATS服务器
trap stop_nats_server EXIT

# 启动NATS服务器
start_nats_server

# 安装依赖
print_header "检查和更新依赖"
go mod tidy

# 运行全部NATS测试
print_header "运行全部NATS测试"
# 只运行非JetStream测试
run_test "./internal/bus/nats" "TestNatsBus_[^J]" "$COVERAGE_DIR/nats.out" "30s"

# 可选: 分别运行标准NATS测试和JetStream测试
# print_header "运行标准NATS测试"
# run_test "./internal/bus/nats" "TestNatsBus_[^J]" "$COVERAGE_DIR/nats_standard.out" "30s"

# print_header "运行JetStream测试"
# run_test "./internal/bus/nats" "TestNatsBus_JetStream" "$COVERAGE_DIR/nats_jetstream.out" "30s"

# 显示测试覆盖率报告
print_header "测试覆盖率报告"
go tool cover -func="$COVERAGE_DIR/nats.out"

# 可选: 生成HTML覆盖率报告
HTML_COVERAGE="$COVERAGE_DIR/coverage.html"
go tool cover -html="$COVERAGE_DIR/nats.out" -o="$HTML_COVERAGE"
echo -e "\n${GREEN}覆盖率HTML报告已生成: $HTML_COVERAGE${NC}\n"
echo "可以使用浏览器打开查看: open $HTML_COVERAGE"

# 提示
echo -e "\n${GREEN}所有测试已完成!${NC}\n" 