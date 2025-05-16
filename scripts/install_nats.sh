#!/bin/bash
set -e

# 颜色和格式
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# 标题输出函数
function print_header() {
    echo -e "\n${BOLD}${YELLOW}=> $1${NC}\n"
}

# 检查操作系统
OS=$(uname -s)
print_header "检测到操作系统: $OS"

# 根据不同操作系统安装NATS服务器
case "$OS" in
    Darwin)
        print_header "使用Homebrew安装NATS服务器"
        if ! command -v brew &> /dev/null; then
            echo -e "${RED}未找到Homebrew, 请先安装Homebrew:${NC}"
            echo 'bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'
            exit 1
        fi

        brew install nats-server
        ;;
    Linux)
        print_header "在Linux上安装NATS服务器"
        
        if command -v apt-get &> /dev/null; then
            print_header "使用apt安装"
            echo "deb https://packagecloud.io/nats-io/nats-server/ubuntu/ $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/nats-server.list
            curl -L https://packagecloud.io/nats-io/nats-server/gpgkey | sudo apt-key add -
            sudo apt-get update
            sudo apt-get install nats-server
        elif command -v yum &> /dev/null; then
            print_header "使用yum安装"
            echo '[nats-io]
name=NATS Server Repository
baseurl=https://packagecloud.io/nats-io/nats-server/el/7/$basearch
gpgcheck=0
enabled=1' | sudo tee /etc/yum.repos.d/nats-server.repo
            sudo yum install nats-server
        else
            print_header "使用Go安装"
            # 检查是否安装了Go
            if ! command -v go &> /dev/null; then
                echo -e "${RED}未找到Go, 请先安装Go:${NC}"
                echo "https://golang.org/doc/install"
                exit 1
            fi
            
            go install github.com/nats-io/nats-server/v2@latest
            echo "NATS服务器已安装到 $(go env GOPATH)/bin/nats-server"
            echo "请确保 $(go env GOPATH)/bin 在您的PATH环境变量中"
        fi
        ;;
    *)
        print_header "使用Go安装NATS服务器"
        # 检查是否安装了Go
        if ! command -v go &> /dev/null; then
            echo -e "${RED}未找到Go, 请先安装Go:${NC}"
            echo "https://golang.org/doc/install"
            exit 1
        fi
        
        go install github.com/nats-io/nats-server/v2@latest
        echo "NATS服务器已安装到 $(go env GOPATH)/bin/nats-server"
        echo "请确保 $(go env GOPATH)/bin 在您的PATH环境变量中"
        ;;
esac

# 检查安装是否成功
if command -v nats-server &> /dev/null; then
    echo -e "\n${GREEN}NATS服务器安装成功!${NC}"
    echo "版本信息:"
    nats-server -v
    echo -e "\n${YELLOW}现在您可以运行测试脚本:${NC}"
    echo "./scripts/test_bus.sh"
else
    echo -e "\n${RED}NATS服务器安装失败，请手动安装:${NC}"
    echo "- 访问: https://docs.nats.io/running-a-nats-service/introduction/installation"
    echo "- 或者: go install github.com/nats-io/nats-server/v2@latest"
fi 