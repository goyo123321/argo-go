# 构建阶段
FROM golang:1.21-alpine AS builder

WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译应用
RUN go build -o app main.go

# 运行阶段 - 使用更小的基础镜像
FROM alpine:3.20

# 安装最小依赖集
RUN apk add --no-cache \
    bash \
    curl \
    jq \
    iproute2

WORKDIR /app

# 从构建阶段复制可执行文件
COPY --from=builder /app/app /app/

# 创建应用运行所需的目录
RUN mkdir -p /app/tmp

# 设置环境变量
ENV USER=root

# 暴露端口
EXPOSE 3000 7860

# 运行应用
ENTRYPOINT ["./app"]
