Go 代理服务器

这是一个基于 Go 语言的高性能代理服务器，支持 Xray、哪吒监控和 Cloudflare Argo 隧道。

🌟 功能特性

· 🚀 高性能：Go 语言原生支持高并发，性能优越
· 🔒 多协议支持：支持 VLESS、VMESS、Trojan 协议
· 📡 哪吒监控集成：内置哪吒监控客户端，支持 v0/v1 版本
· 🌐 Cloudflare Argo：支持 Argo 隧道，提供免费的 CDN 加速
· 📊 自动订阅：自动生成订阅链接，支持 base64 编码
· 🐳 容器化部署：完整的 Docker 支持，一键部署
· 🔑 智能 UUID：自动生成 UUID，支持环境变量覆盖
· 🛡️ 安全可靠：非 root 用户运行，完善的错误处理

📦 快速开始

使用 Docker Compose（推荐）

```bash
# 1. 克隆项目
git clone <your-repo-url>
cd proxy-server

# 2. 复制环境变量模板
cp .env.example .env

# 3. 编辑 .env 文件，配置你的参数
# 主要配置项：
# UUID - 留空则自动生成
# ARGO_AUTH - Argo 隧道认证信息
# NEZHA_KEY - 哪吒监控密钥

# 4. 启动服务
docker-compose up -d

# 5. 查看日志
docker-compose logs -f
```

使用 Docker 直接运行

```bash
# 简单运行（自动生成 UUID）
docker run -d \
  --name proxy-server \
  -p 7860:7860 \
  -p 3000:3000 \
  ghcr.io/goyo123321/app-go:latest

# 自定义配置运行
docker run -d \
  --name proxy-server \
  -p 7860:7860 \
  -p 3000:3000 \
  -e UUID="your-uuid-here" \
  -e ARGO_AUTH="your-argo-token" \
  -e NEZHA_SERVER="nezha.cc:5555" \
  -e NEZHA_KEY="your-nezha-key" \
  ghcr.io/your-username/proxy-server:latest
```

手动构建运行

```bash
# 1. 安装 Go 环境（1.21+）
# 从 https://golang.org/dl/ 下载安装

# 2. 克隆项目
git clone <your-repo-url>
cd proxy-server

# 3. 设置环境变量
export UUID="your-uuid-here"
export ARGO_AUTH="your-argo-token"

# 4. 运行
go run main.go

# 或编译后运行
go build -o proxy-server main.go
./proxy-server
```

⚙️ 环境变量配置

所有配置都通过环境变量进行，以下是最重要的配置项：

环境变量 说明 默认值 是否必需
UUID Xray 用户 UUID 自动生成 ❌
ARGO_DOMAIN Argo 隧道域名 无 ❌
ARGO_AUTH Argo 认证信息（Token 或 JSON） 无 ❌
NEZHA_SERVER 哪吒监控服务器地址 无 ❌
NEZHA_KEY 哪吒监控客户端密钥 无 ❌
NEZHA_PORT 哪吒监控服务器端口 无 ❌
CFIP CDN 回源 IP 地址 cdns.doon.eu.org ❌
CFPORT CDN 回源端口 443 ❌
NAME 节点名称前缀 无 ❌
UPLOAD_URL 节点上传地址 无 ❌
PROJECT_URL 项目访问地址 无 ❌
SUB_PATH 订阅链接访问路径 sub ❌
PORT HTTP 服务端口 3000 ❌
EXTERNAL_PORT 外部代理端口 7860 ❌

配置示例

1. 基础配置（无 UUID，自动生成）

```bash
docker run -d \
  -p 7860:7860 \
  -p 3000:3000 \
  proxy-server:latest
```

2. 使用固定 UUID

```bash
docker run -d \
  -p 7860:7860 \
  -p 3000:3000 \
  -e UUID="4b3e2bfe-bde1-5def-d035-0cb572bbd046" \
  proxy-server:latest
```

3. 完整配置

```bash
docker run -d \
  -p 7860:7860 \
  -p 3000:3000 \
  -e UUID="your-uuid" \
  -e ARGO_DOMAIN="your-domain.com" \
  -e ARGO_AUTH="your-argo-token" \
  -e NEZHA_SERVER="nezha.cc:5555" \
  -e NEZHA_KEY="your-secret-key" \
  -e NAME="US-01" \
  proxy-server:latest
```

🔗 订阅链接

服务启动后，可以通过以下方式获取订阅：

1. Web 访问

```
http://你的域名或IP:7680/sub
```

2. 直接获取

```bash
# 从服务器日志中查找订阅链接
docker logs proxy-server | grep "订阅内容"

# 或者直接访问
curl http://localhost:7860/sub
```

3. 订阅格式

订阅链接是 base64 编码的，包含三种协议：

· VLESS 协议
· VMESS 协议
· Trojan 协议

🛠️ 高级配置

Cloudflare Argo 隧道

使用 Token 连接

```bash
# 从 Cloudflare 面板获取 Token
-e ARGO_AUTH="your-argo-token-here"
```

使用 JSON 配置文件

```bash
# 将 JSON 配置作为环境变量传入
-e ARGO_AUTH='{"TunnelSecret":"...","TunnelID":"...","TunnelName":"..."}'
```

哪吒监控

v1 版本（推荐）

```bash
-e NEZHA_SERVER="nezha.cc:5555"
-e NEZHA_KEY="your-key-here"
# NEZHA_PORT 留空
```

v0 版本

```bash
-e NEZHA_SERVER="nezha.cc"
-e NEZHA_PORT="5555"
-e NEZHA_KEY="your-key-here"
```

节点上传

如果需要将节点上传到 Merge-sub 项目：

```bash
# 设置上传地址
-e UPLOAD_URL="https://merge.xxx.com"
-e PROJECT_URL="https://your-domain.com"
```

📁 目录结构

```
proxy-server/
├── main.go              # 主程序源码
├── Dockerfile          # Docker 构建文件
├── docker-compose.yml  # Docker Compose 配置
├── go.mod             # Go 模块定义
├── go.sum             # 依赖校验和
├── .env.example       # 环境变量示例
├── index.html         # 首页文件（可选）
└── README.md          # 本文件
```

🐳 Docker 部署

构建镜像

```bash
# 构建本地镜像
docker build -t proxy-server:local .

# 多平台构建（amd64 + arm64）
docker buildx build --platform linux/amd64,linux/arm64 \
  -t proxy-server:multiarch .
```

使用 Docker Compose

```yaml
# docker-compose.yml 示例
version: '3.8'
services:
  proxy:
    build: .
    ports:
      - "7860:7860"
      - "3000:3000"
    environment:
      - UUID=${UUID:-}
      - ARGO_AUTH=${ARGO_AUTH:-}
    restart: unless-stopped
```

持久化数据

如果需要保存订阅文件：

```bash
docker run -d \
  -v ./data:/tmp/app \
  -p 7860:7860 \
  proxy-server:latest
```

🔍 监控与日志

查看日志

```bash
# Docker Compose
docker-compose logs -f

# Docker
docker logs -f proxy-server

# 查看实时日志
docker logs --tail 100 -f proxy-server
```

健康检查

服务内置健康检查，可以通过以下方式检查状态：

```bash
# 检查 HTTP 服务
curl http://localhost:7860/

# 检查订阅服务
curl http://localhost:7860/sub

# Docker 健康状态
docker inspect --format='{{.State.Health.Status}}' proxy-server
```

🚨 故障排除

常见问题

1. 端口被占用

```bash
# 检查端口占用
netstat -tlnp | grep :7860
netstat -tlnp | grep :3000

# 停止占用进程或修改端口
export PORT=3001
export EXTERNAL_PORT=7861
```

2. UUID 相关问题

```bash
# 查看当前使用的 UUID
docker logs proxy-server | grep "UUID"

# 重新生成 UUID（删除容器重新运行）
docker rm -f proxy-server
docker run -d -p 7860:7860 proxy-server:latest
```

3. Argo 隧道连接失败

```bash
# 检查日志
docker logs proxy-server | grep -i "argo\|tunnel"

# 验证 Token 是否正确
# 确保 ARGO_AUTH 环境变量设置正确
```

4. 哪吒监控无法连接

```bash
# 检查服务器地址和密钥
# 确保网络可以访问哪吒服务器
# 检查防火墙设置
```

日志级别

程序会输出详细的日志，主要关注以下关键词：

· ERROR - 错误信息
· UUID - UUID 相关信息
· ArgoDomain - Argo 域名信息
· 订阅内容 - 订阅链接信息

📄 许可证

本项目采用 MIT 许可证。详情请见 LICENSE 文件。

🤝 贡献

欢迎提交 Issue 和 Pull Request！

1. Fork 本仓库
2. 创建功能分支
3. 提交更改
4. 推送到分支
5. 创建 Pull Request

📞 支持

如果您遇到问题或有建议：

1. 查看 Issues
2. 提交新的 Issue
3. 提供详细的错误信息和日志

🎯 版本历史

v1.0.0 (2024-01-18)

· 初始版本发布
· 支持 VLESS、VMESS、Trojan 协议
· 集成哪吒监控客户端
· 支持 Cloudflare Argo 隧道
· 自动订阅生成
· Docker 容器化支持

🙏 致谢

· Xray-core
· 哪吒监控
· Cloudflare Argo Tunnel
· 所有贡献者和用户

---

提示：本工具仅供学习和合法用途，请遵守当地法律法规。
