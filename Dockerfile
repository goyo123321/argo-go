# 使用官方 Go 镜像
FROM golang:1.21-alpine

# 安装必要的工具和监控依赖
RUN apk add --no-cache \
    git \
    curl \
    bash \
    ifstat \
    jq \
    && echo "安装监控依赖完成"

# 设置工作目录
WORKDIR /app

# 复制 Go 模块文件
COPY go.mod go.sum ./

# 设置环境变量
ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on
ENV USER=root  # 解决监控脚本中 USER 变量未定义的问题

# 复制源代码
COPY . .

# 编译应用 - 输出文件名为 app
RUN go build -o app main.go

# 创建必要的目录结构（根据日志需要）
RUN mkdir -p ./tmp

# 暴露端口
EXPOSE 3000 7860

# 运行应用
CMD ["./app"]
