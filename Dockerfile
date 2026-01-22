# 构建阶段
FROM golang:1.21-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY . .
RUN go build -o app main.go

# 运行阶段
FROM alpine:3.20
# 安装运行所需的依赖
RUN apk add --no-cache \
    bash \
    curl \
    jq \
    iproute2 \
    && rm -rf /var/cache/apk/*
WORKDIR /app
COPY --from=builder /app/app .
COPY --from=builder /app/tmp ./tmp
EXPOSE 3000 7860
CMD ["./app"]
