FROM golang:1.21-alpine AS builder

WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY main.go ./

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o app-go .

# 运行阶段
FROM alpine:latest

RUN apk --no-cache add ca-certificates curl

WORKDIR /root/

# 从构建阶段复制二进制文件
COPY --from=builder /app/app-go .

# 创建数据目录
RUN mkdir -p /data

# 暴露端口
EXPOSE 3000 7860

# 设置环境变量
ENV FILE_PATH=/data \
    PORT=3000 \
    EXTERNAL_PORT=7860 \
    UUID=$(uuidgen)

# 启动命令
CMD ["./app-go"]
