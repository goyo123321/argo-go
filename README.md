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

⚙️ 环境变量配置

所有配置都通过环境变量进行，以下是完整的配置表格：

环境变量 类型 默认值 说明 必需
## 环境变量

| 变量名 | 是否必须 | 默认值 | 说明 |
|--------|----------|--------|------|
| UPLOAD_URL | 否 | - | 订阅上传地址 |
| PROJECT_URL | 否 | https://www.google.com | 项目分配的域名 |
| AUTO_ACCESS | 否 | false | 是否开启自动访问保活 |
| PORT | 否 | 3000 | HTTP服务监听端口 |
| EXTERNAL_PORT | 否 | 7860默认 | Argo隧道端口和外部代理服务器端口 |
| UUID | 否 | 4b3e2bfe-bde1-5def-d035-0cb572bbd046 | 用户UUID |
| NEZHA_SERVER | 否 | - | 哪吒面板域名 |
| NEZHA_PORT | 否 | - | 哪吒端口 |
| NEZHA_KEY | 否 | - | 哪吒密钥 |
| ARGO_DOMAIN | 否 | - | Argo固定隧道域名 |
| ARGO_AUTH | 否 | - | Argo固定隧道密钥 |
| CFIP | 否 | www.visa.com.tw | 节点优选域名或IP |
| CFPORT | 否 | 443 | 节点端口 |
| NAME | 否 | Vls | 节点名称前缀 |
| FILE_PATH | 否 | ./tmp | 运行目录 |
| SUB_PATH | 否 | sub | 订阅路径 |
基础配置    

📦 快速开始

使用 Docker Compose（推荐）

```bash
# 1. 克隆项目
git clone <your-repo-url>
cd proxy-server

# 2. 复制环境变量模板
cp .env.example .env

# 3. 编辑 .env 文件，配置你的参数
nano .env

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
  ghcr.io/goyo123321/app-go:latest
```

.env 文件示例

```env
# 基础配置
UUID=4b3e2bfe-bde1-5def-d035-0cb572bbd046
SUB_PATH=sub
PORT=3000
EXTERNAL_PORT=7860
FILE_PATH=/tmp/app

# Cloudflare Argo 配置
ARGO_DOMAIN=your-domain.com
ARGO_AUTH=your-argo-token-here

# 哪吒监控配置
NEZHA_SERVER=nezha.cc:5555
NEZHA_KEY=your-secret-key-here

# CDN 配置
CFIP=cdns.doon.eu.org
CFPORT=443

# 节点配置
NAME=US-01
UPLOAD_URL=https://merge.xxx.com
PROJECT_URL=https://your-project.herokuapp.com
AUTO_ACCESS=true
```

🔗 订阅链接

服务启动后，可以通过以下方式获取订阅：

1. Web 访问

```
http://你的域名或IP:7860/sub
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

🛠️ 配置示例

场景 1：基本使用（自动生成 UUID）

```bash
docker run -d \
  -p 7860:7860 \
  -p 3000:3000 \
  proxy-server:latest
```

场景 2：使用固定 UUID

```bash
docker run -d \
  -p 7860:7860 \
  -p 3000:3000 \
  -e UUID="4b3e2bfe-bde1-5def-d035-0cb572bbd046" \
  proxy-server:latest
```

场景 3：完整配置

```bash
docker run -d \
  -p 7860:7860 \
  -p 3000:3000 \
  -e UUID="your-uuid" \
  -e ARGO_DOMAIN="your-domain.com" \
  -e ARGO_AUTH="your-argo-token" \
  -e NEZHA_SERVER="nezha.cc:5555" \
  -e NEZHA_KEY="your-secret-key" \
  -e CFIP="cdn.example.com" \
  -e CFPORT="8443" \
  -e NAME="US-01" \
  -e UPLOAD_URL="https://merge.example.com" \
  -e AUTO_ACCESS="true" \
  proxy-server:latest
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
      - NEZHA_SERVER=${NEZHA_SERVER:-}
      - NEZHA_KEY=${NEZHA_KEY:-}
    restart: unless-stopped
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
curl http://localhost:3000/

# 检查订阅服务
curl http://localhost:7860/sub

# Docker 健康状态
docker inspect --format='{{.State.Health.Status}}' proxy-server
```

🚨 故障排除

常见问题

问题 可能原因 解决方案
端口被占用 其他服务占用了相同端口 修改端口配置或停止占用进程
UUID 无效 环境变量中的 UUID 格式错误 使用有效的 UUID 或留空自动生成
Argo 隧道连接失败 Token 无效或网络问题 检查 Token 正确性和网络连接
哪吒监控无法连接 服务器地址或密钥错误 检查服务器地址和密钥配置
订阅链接无法访问 服务未启动或配置错误 检查日志确认服务状态

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
