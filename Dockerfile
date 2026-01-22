# 使用官方 Go 镜像
FROM golang:1.21-alpine

# 安装必要的工具
RUN apk add --no-cache git curl bash

# 设置工作目录
WORKDIR /app

# 复制 Go 模块文件
COPY go.mod go.sum ./

# 设置环境变量
ENV GOPROXY=https://goproxy.cn,direct
ENV GO111MODULE=on

# 复制源代码
COPY . .

# 编译应用
RUN go build -o go-app main.go

# 暴露端口
EXPOSE 3000 7860

# 运行应用
CMD ["./go-app"]
