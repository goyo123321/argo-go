FROM golang:1.21-alpine AS builder

# 安装编译依赖
RUN apk add --no-cache git build-base

# 设置工作目录
WORKDIR /app

# 复制go模块文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o tunnel-server .

# 运行时镜像
FROM alpine:latest

# 安装运行时依赖
RUN apk --no-cache add ca-certificates tzdata \
    && mkdir /app

# 设置时区
ENV TZ=Asia/Shanghai

# 设置工作目录
WORKDIR /app

# 复制可执行文件
COPY --from=builder /app/tunnel-server .

# 创建必要目录
RUN mkdir -p /app/tmp

# 暴露端口
EXPOSE 3000 7860

# 运行应用
CMD ["./tunnel-server"]
