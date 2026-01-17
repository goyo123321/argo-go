#!/bin/bash
set -e

# 如果存在自定义配置文件，则使用
if [ -f "/app/config.yaml" ]; then
    echo "使用自定义配置文件"
    # 这里可以添加从配置文件加载环境变量的逻辑
    # 例如使用 yq 或类似工具解析 yaml
elif [ -f "/app/config.json" ]; then
    echo "使用自定义JSON配置文件"
fi

# 设置默认值
export FILE_PATH=${FILE_PATH:-"/app/tmp"}
export SUB_PATH=${SUB_PATH:-"sub"}
export PORT=${PORT:-3000}
export EXTERNAL_PORT=${EXTERNAL_PORT:-7860}
export UUID=${UUID:-"35461c1b-c9fb-efd5-e5d4-cf754d37bd4b"}
export NEZHA_SERVER=${NEZHA_SERVER:-""}
export NEZHA_PORT=${NEZHA_PORT:-""}
export NEZHA_KEY=${NEZHA_KEY:-""}
export ARGO_DOMAIN=${ARGO_DOMAIN:-""}
export ARGO_AUTH=${ARGO_AUTH:-""}
export ARGO_PORT=${ARGO_PORT:-7860}
export CFIP=${CFIP:-"cdns.doon.eu.org"}
export CFPORT=${CFPORT:-443}
export NAME=${NAME:-""}
export UPLOAD_URL=${UPLOAD_URL:-""}
export PROJECT_URL=${PROJECT_URL:-""}
export AUTO_ACCESS=${AUTO_ACCESS:-"false"}
export DAEMON_CHECK_INTERVAL=${DAEMON_CHECK_INTERVAL:-30000}
export DAEMON_MAX_RETRIES=${DAEMON_MAX_RETRIES:-5}
export DAEMON_RESTART_DELAY=${DAEMON_RESTART_DELAY:-10000}

# 创建必要的目录
mkdir -p ${FILE_PATH}
mkdir -p /app/logs

# 设置权限
chown -R tunnel:tunnel ${FILE_PATH} || true
chmod -R 755 ${FILE_PATH} || true

# 打印环境变量（调试用）
echo "=== 环境变量配置 ==="
echo "FILE_PATH: ${FILE_PATH}"
echo "PORT: ${PORT}"
echo "EXTERNAL_PORT: ${EXTERNAL_PORT}"
echo "UUID: ${UUID}"
echo "ARGO_DOMAIN: ${ARGO_DOMAIN}"
echo "NEZHA_SERVER: ${NEZHA_SERVER}"
echo "=========================="

# 执行主程序
exec "$@"
