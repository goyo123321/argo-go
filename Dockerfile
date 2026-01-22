# 多阶段构建版本，减小镜像大小
# 第一阶段：构建
FROM golang:1.21-alpine AS builder

# 安装必要的工具
RUN apk add --no-cache git curl bash

# 设置工作目录
WORKDIR /app

# 复制 Go 模块文件
COPY go.mod go.sum ./

# 设置环境变量
ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 编译应用
RUN go build -o go-app main.go

# 第二阶段：运行
FROM alpine:latest

# 安装运行时需要的依赖
RUN apk --no-cache add ca-certificates tzdata

# 设置工作目录
WORKDIR /root/

# 从构建阶段复制可执行文件
COPY --from=builder /app/go-app .

# 暴露端口
EXPOSE 3000 7860

# 运行应用
CMD ["./go-app"]
