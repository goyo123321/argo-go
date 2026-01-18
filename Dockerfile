# 使用官方 Go 镜像作为构建环境
FROM golang:1.21-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum 文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译应用
RUN go build -o proxy-server main.go

# 使用轻量级的基础镜像运行应用
FROM alpine:latest

# 安装必要的工具
RUN apk --no-cache add ca-certificates

# 设置工作目录
WORKDIR /root/

# 从构建阶段复制可执行文件
COPY --from=builder /app/proxy-server .

# 暴露端口
EXPOSE 3000 7860

# 运行应用
CMD ["./proxy-server"]
