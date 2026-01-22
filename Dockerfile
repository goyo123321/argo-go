# 使用官方 Go 镜像
FROM golang:1.21-alpine

# 安装必要的工具
RUN apk add --no-cache \
    git \
    bash \
    curl \
    jq \
    iproute2 \
    net-tools

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译应用
RUN go build -o app main.go

# 创建必要的目录结构
RUN mkdir -p ./tmp

# 暴露端口
EXPOSE 3000 7860

# 设置默认用户
ENV USER=root

# 运行应用
CMD ["./app"]
