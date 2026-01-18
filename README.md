Go 代理服务器

一个基于 Go 语言的高性能代理服务器，支持 Xray、哪吒监控和 Cloudflare Argo 隧道。

功能特性

· 🚀 高性能 Go 语言实现
· 🔒 支持 VLESS、VMESS、Trojan 协议
· 📡 集成哪吒监控客户端
· 🌐 Cloudflare Argo 隧道支持
· 📊 自动订阅生成
· 🐳 Docker 容器化部署
· 🔄 UUID 自动生成（无需配置）

快速开始

使用 Docker

```bash
# 拉取镜像
docker pull ghcr.io/goyo123321/app-go2:latest

# 运行容器
docker run -d \
  --name proxy-server \
  -p 7860:7860 \
  -p 3000:3000 \
  ghcr.io/goyo123321/app-go2:latest
```

使用 Docker Compose

```yaml
version: '3.8'
services:
  proxy-server:
    image: ghcr.io/goyo123321/app-go2:latest
    container_name: proxy-server
    restart: unless-stopped
    ports:
      - "7860:7860"
      - "3000:3000"
    environment:
      # UUID 可选，不设置会自动生成
      - UUID=
      # 哪吒监控配置（可选）
      - NEZHA_SERVER=
      - NEZHA_KEY=
      # Cloudflare Argo（可选）
      - ARGO_DOMAIN=
      - ARGO_AUTH=
```

环境变量

变量名 说明 默认值
UUID Xray 用户 UUID 自动生成
NEZHA_SERVER 哪吒监控服务器地址 无
NEZHA_KEY 哪吒监控密钥 无
ARGO_DOMAIN Argo 隧道域名 无
ARGO_AUTH Argo 认证信息 无
CFIP CDN 回源地址 cdns.doon.eu.org
CFPORT CDN 回源端口 443
NAME 节点名称前缀 无

API 接口

· GET / - 首页（显示 Hello world! 或 index.html）
· GET /sub - 订阅链接（Base64 编码）

构建镜像

```bash
# 克隆项目
git clone <repository-url>
cd argo-go

# 构建镜像
docker build -t proxy-server .

# 运行测试
docker run -d -p 7860:7860 -p 3000:3000 proxy-server
```

部署平台

Railway

https://railway.app/button.svg

Heroku

```bash
heroku container:push web
heroku container:release web
```

许可证

MIT License
