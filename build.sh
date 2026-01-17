#!/bin/bash

# 构建脚本
VERSION="1.0.0"
APP_NAME="tunnel-server"

echo "🚀 开始构建隧道服务器..."

# 清理旧构建
echo "🧹 清理旧构建..."
rm -rf build/
mkdir -p build/

# 目标平台列表
PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "windows/amd64"
    "windows/386"
)

# 构建所有平台
for platform in "${PLATFORMS[@]}"
do
    platform_split=(${platform//\// })
    GOOS=${platform_split[0]}
    GOARCH=${platform_split[1]}
    
    output_name="${APP_NAME}-${VERSION}-${GOOS}-${GOARCH}"
    if [ $GOOS = "windows" ]; then
        output_name+='.exe'
    fi
    
    echo "🔨 构建 $GOOS/$GOARCH..."
    
    # 设置环境变量
    export GOOS=$GOOS
    export GOARCH=$GOARCH
    
    # 构建
    go build -ldflags="-s -w" -o build/$output_name
    
    # 压缩 (如果有upx)
    if command -v upx &> /dev/null; then
        echo "📦 压缩 $output_name..."
        upx --best --lzma build/$output_name
    fi
    
    echo "✅ $GOOS/$GOARCH 构建完成"
done

# 生成版本信息
echo "📝 生成版本信息..."
cat > build/VERSION.txt << EOF
隧道服务器 ${VERSION}
构建时间: $(date)
Git提交: $(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
EOF

# 生成配置文件示例
cat > build/config.env.example << 'EOF'
# 隧道服务器配置示例

# 基本配置
UUID=35461c1b-c9fb-efd5-e5d4-cf754d37bd4b
FILE_PATH=./tmp
SUB_PATH=sub
PORT=3000
EXTERNAL_PORT=7860

# 哪吒监控
NEZHA_SERVER=nezha.example.com:443
NEZHA_KEY=your-nezha-key

# Cloudflare隧道配置 (可选)
ARGO_AUTH={"TunnelSecret":"xxx","TunnelID":"xxx","AccountTag":"xxx"}
ARGO_DOMAIN=tunnel.example.com

# 订阅配置
CFIP=cdn.example.com
CFPORT=443
NAME=MyTunnel

# 守护进程配置
DAEMON_CHECK_INTERVAL=30000
DAEMON_MAX_RETRIES=5
DAEMON_RESTART_DELAY=10000
EOF

# 生成README
cat > build/README.md << 'EOF'
# 隧道服务器使用说明

## 快速开始

1. 下载对应平台的可执行文件
2. 配置环境变量 (参考 config.env.example)
3. 运行程序: `./tunnel-server`

## 常用命令

```bash
# 查看守护进程状态
curl http://localhost:3000/daemon-status

# 下载订阅
curl http://localhost:3000/sub -o subscription.txt

# 重启服务
curl -X POST http://localhost:3000/restart/xray
