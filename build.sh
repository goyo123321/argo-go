#!/bin/bash

# 构建脚本
set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 打印函数
print_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# 检查 Docker 是否安装
check_docker() {
    if ! command -v docker &> /dev/null; then
        print_error "Docker 未安装！"
        exit 1
    fi
}

# 检查 Docker Compose 是否安装
check_docker_compose() {
    if ! command -v docker-compose &> /dev/null; then
        print_warn "Docker Compose 未安装，将尝试使用 docker compose 命令"
        if ! docker compose version &> /dev/null; then
            print_error "Docker Compose 未安装！"
            exit 1
        fi
    fi
}

# 构建镜像
build_image() {
    local tag=$1
    local platform=$2
    
    print_info "开始构建镜像: $tag"
    
    if [ -n "$platform" ]; then
        print_info "构建平台: $platform"
        docker build --platform $platform -t $tag .
    else
        docker build -t $tag .
    fi
    
    if [ $? -eq 0 ]; then
        print_info "镜像构建成功: $tag"
    else
        print_error "镜像构建失败"
        exit 1
    fi
}

# 多平台构建
build_multi_platform() {
    local repository=$1
    local version=$2
    
    print_info "开始多平台构建..."
    
    # 创建构建器实例
    docker buildx create --name tunnel-builder --use || true
    docker buildx inspect --bootstrap
    
    # 构建并推送多平台镜像
    docker buildx build \
        --platform linux/amd64,linux/arm64 \
        -t ${repository}:${version} \
        -t ${repository}:latest \
        --push .
    
    print_info "多平台构建完成"
}

# 运行容器
run_container() {
    local image=$1
    
    print_info "启动容器..."
    
    if [ -f "docker-compose.yml" ]; then
        if command -v docker-compose &> /dev/null; then
            docker-compose up -d
        else
            docker compose up -d
        fi
    else
        docker run -d \
            --name tunnel-server \
            -p 3000:3000 \
            -p 7860:7860 \
            -e UUID=${UUID} \
            -e NEZHA_SERVER=${NEZHA_SERVER} \
            -e NEZHA_KEY=${NEZHA_KEY} \
            -v $(pwd)/data:/app/data \
            ${image}
    fi
    
    print_info "容器启动完成"
}

# 清理旧的构建
cleanup() {
    print_info "清理旧镜像..."
    
    # 停止并删除容器
    docker stop tunnel-server 2>/dev/null || true
    docker rm tunnel-server 2>/dev/null || true
    
    # 删除未使用的镜像
    docker image prune -f
    
    print_info "清理完成"
}

# 显示帮助
show_help() {
    echo "使用说明:"
    echo "  ./build.sh [选项]"
    echo ""
    echo "选项:"
    echo "  build [tag]          构建镜像（默认tag: tunnel-server:latest）"
    echo "  multi [repo] [ver]   多平台构建并推送"
    echo "  run [image]          运行容器"
    echo "  all                  清理、构建并运行"
    echo "  cleanup              清理旧镜像和容器"
    echo "  help                 显示此帮助信息"
    echo ""
    echo "环境变量:"
    echo "  UUID                 隧道UUID（默认自动生成）"
    echo "  NEZHA_SERVER         哪吒监控服务器"
    echo "  NEZHA_KEY            哪吒监控密钥"
    echo "  ARGO_DOMAIN          Cloudflare隧道域名"
    echo "  ARGO_AUTH            Cloudflare认证信息"
}

# 生成随机UUID
generate_uuid() {
    if [ -z "$UUID" ]; then
        UUID=$(cat /proc/sys/kernel/random/uuid 2>/dev/null || \
               uuidgen 2>/dev/null || \
               echo "35461c1b-c9fb-efd5-e5d4-cf754d37bd4b")
        export UUID
        print_info "生成UUID: $UUID"
    fi
}

# 主函数
main() {
    check_docker
    check_docker_compose
    
    case "$1" in
        "build")
            local tag=${2:-"tunnel-server:latest"}
            build_image "$tag" "$3"
            ;;
        "multi")
            local repo=${2:-"your-dockerhub/tunnel-server"}
            local ver=${3:-"1.0.0"}
            build_multi_platform "$repo" "$ver"
            ;;
        "run")
            local image=${2:-"tunnel-server:latest"}
            run_container "$image"
            ;;
        "all")
            generate_uuid
            cleanup
            build_image "tunnel-server:latest"
            run_container "tunnel-server:latest"
            ;;
        "cleanup")
            cleanup
            ;;
        "help"|"-h"|"--help")
            show_help
            ;;
        *)
            print_info "执行完整构建流程..."
            generate_uuid
            cleanup
            build_image "tunnel-server:latest"
            run_container "tunnel-server:latest"
            ;;
    esac
}

# 执行主函数
main "$@"
