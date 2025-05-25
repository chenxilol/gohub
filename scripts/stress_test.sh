#!/bin/bash

# GoHub 压力测试脚本
# 用法: ./scripts/stress_test.sh [测试类型] [强度级别]

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 显示使用帮助
show_help() {
    echo "GoHub 压力测试脚本"
    echo ""
    echo "用法: $0 [测试类型] [强度级别]"
    echo ""
    echo "测试类型:"
    echo "  basic     - 基础功能测试"
    echo "  extreme   - 极端压力测试"
    echo "  memory    - 内存泄漏检测"
    echo "  stability - 长时间稳定性测试"
    echo "  benchmark - 性能基准测试"
    echo "  all       - 运行所有测试"
    echo ""
    echo "强度级别:"
    echo "  low       - 低强度 (适合开发环境)"
    echo "  medium    - 中等强度 (适合测试环境)"
    echo "  high      - 高强度 (适合生产环境验证)"
    echo ""
    echo "示例:"
    echo "  $0 basic low"
    echo "  $0 extreme high"
    echo "  $0 all medium"
}

# 检查Go环境
check_go_env() {
    if ! command -v go &> /dev/null; then
        log_error "Go 未安装或不在 PATH 中"
        exit 1
    fi
    
    log_info "Go 版本: $(go version)"
}

# 检查系统资源
check_system_resources() {
    log_info "=== 系统资源检查 ==="
    
    # CPU信息
    if command -v nproc &> /dev/null; then
        log_info "CPU 核心数: $(nproc)"
    fi
    
    # 内存信息
    if [[ "$OSTYPE" == "linux-gnu"* ]]; then
        MEM_GB=$(free -g | awk '/^Mem:/{print $2}')
        log_info "总内存: ${MEM_GB}GB"
    elif [[ "$OSTYPE" == "darwin"* ]]; then
        MEM_GB=$(system_profiler SPHardwareDataType | grep "Memory:" | awk '{print $2}')
        log_info "总内存: ${MEM_GB}GB"
    fi
    
    # 磁盘空间
    DISK_AVAIL=$(df -h . | awk 'NR==2{print $4}')
    log_info "可用磁盘空间: $DISK_AVAIL"
}

# 运行基础测试
run_basic_tests() {
    log_info "=== 运行基础功能测试 ==="
    cd cmd/gohub
    go test -v -run="TestServerWithSDK" -timeout=10m
    log_success "基础功能测试完成"
}

# 运行极端压力测试
run_extreme_tests() {
    local intensity=$1
    log_info "=== 运行极端压力测试 (强度: $intensity) ==="
    
    cd cmd/gohub
    
    # 根据强度设置不同的测试参数
    case $intensity in
        "low")
            GOMAXPROCS=2 go test -v -run="TestExtremeRoomCreation" -timeout=30m
            ;;
        "medium")
            GOMAXPROCS=4 go test -v -run="TestExtremeRoomCreation|TestHighFrequencyBroadcast" -timeout=60m
            ;;
        "high")
            GOMAXPROCS=8 go test -v -run="TestExtremeRoomCreation|TestHighFrequencyBroadcast|TestMassiveRoomOperations" -timeout=120m
            ;;
        *)
            log_error "未知的强度级别: $intensity"
            exit 1
            ;;
    esac
    
    log_success "极端压力测试完成"
}

# 运行内存泄漏检测
run_memory_tests() {
    log_info "=== 运行内存泄漏检测 ==="
    cd cmd/gohub
    
    # 使用内存分析工具
    go test -v -run="TestMemoryLeakDetection" -memprofile=mem.prof -timeout=30m
    
    if [ -f mem.prof ]; then
        log_info "内存分析文件已生成: mem.prof"
        log_info "可以使用以下命令查看内存使用情况:"
        log_info "  go tool pprof mem.prof"
    fi
    
    log_success "内存泄漏检测完成"
}

# 运行长时间稳定性测试
run_stability_tests() {
    local intensity=$1
    log_info "=== 运行长时间稳定性测试 (强度: $intensity) ==="
    
    cd cmd/gohub
    
    case $intensity in
        "low")
            go test -v -run="TestLongTermStability" -timeout=10m
            ;;
        "medium")
            go test -v -run="TestLongTermStability" -timeout=30m
            ;;
        "high")
            go test -v -run="TestLongTermStability" -timeout=60m
            ;;
        *)
            log_error "未知的强度级别: $intensity"
            exit 1
            ;;
    esac
    
    log_success "长时间稳定性测试完成"
}

# 运行性能基准测试
run_benchmark_tests() {
    local intensity=$1
    log_info "=== 运行性能基准测试 (强度: $intensity) ==="
    
    cd cmd/gohub
    
    case $intensity in
        "low")
            go test -bench=BenchmarkExtremeRoomCreation -benchtime=10s -timeout=30m
            ;;
        "medium")
            go test -bench=. -benchtime=30s -timeout=60m
            ;;
        "high")
            go test -bench=. -benchtime=60s -cpu=1,2,4,8 -timeout=120m
            ;;
        *)
            log_error "未知的强度级别: $intensity"
            exit 1
            ;;
    esac
    
    log_success "性能基准测试完成"
}

# 生成测试报告
generate_report() {
    local test_type=$1
    local intensity=$2
    local start_time=$3
    local end_time=$4
    
    local duration=$((end_time - start_time))
    local report_file="stress_test_report_$(date +%Y%m%d_%H%M%S).txt"
    
    {
        echo "======================================="
        echo "       GoHub 压力测试报告"
        echo "======================================="
        echo ""
        echo "测试时间: $(date)"
        echo "测试类型: $test_type"
        echo "强度级别: $intensity"
        echo "测试持续时间: ${duration}秒"
        echo ""
        echo "系统信息:"
        echo "  操作系统: $OSTYPE"
        echo "  Go 版本: $(go version)"
        if command -v nproc &> /dev/null; then
            echo "  CPU 核心数: $(nproc)"
        fi
        echo ""
        echo "测试结果:"
        echo "  状态: 完成"
        echo "  详细日志请查看终端输出"
        echo ""
        echo "建议:"
        echo "  - 如果测试失败，请检查系统资源是否充足"
        echo "  - 建议在生产环境前进行更高强度的测试"
        echo "  - 定期运行稳定性测试以确保服务可靠性"
    } > "$report_file"
    
    log_success "测试报告已生成: $report_file"
}

# 主函数
main() {
    local test_type=${1:-"basic"}
    local intensity=${2:-"low"}
    
    # 显示帮助
    if [[ "$1" == "-h" || "$1" == "--help" ]]; then
        show_help
        exit 0
    fi
    
    # 记录开始时间
    local start_time=$(date +%s)
    
    log_info "开始 GoHub 压力测试"
    log_info "测试类型: $test_type"
    log_info "强度级别: $intensity"
    echo ""
    
    # 环境检查
    check_go_env
    check_system_resources
    echo ""
    
    # 根据测试类型运行相应测试
    case $test_type in
        "basic")
            run_basic_tests
            ;;
        "extreme")
            run_extreme_tests "$intensity"
            ;;
        "memory")
            run_memory_tests
            ;;
        "stability")
            run_stability_tests "$intensity"
            ;;
        "benchmark")
            run_benchmark_tests "$intensity"
            ;;
        "all")
            log_info "运行所有测试类型..."
            run_basic_tests
            run_extreme_tests "$intensity"
            run_memory_tests
            run_stability_tests "$intensity"
            run_benchmark_tests "$intensity"
            ;;
        *)
            log_error "未知的测试类型: $test_type"
            show_help
            exit 1
            ;;
    esac
    
    # 记录结束时间
    local end_time=$(date +%s)
    
    # 生成报告
    generate_report "$test_type" "$intensity" "$start_time" "$end_time"
    
    log_success "所有测试已完成！"
}

# 运行主函数
main "$@" 