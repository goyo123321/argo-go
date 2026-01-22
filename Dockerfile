# 构建阶段
FROM golang:1.21-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY . .
RUN go build -o app main.go

# 运行阶段
FROM alpine:3.20
RUN apk add --no-cache bash curl ifstat jq
WORKDIR /app
COPY --from=builder /app/app .
COPY --from=builder /app/tmp ./tmp
EXPOSE 3000 7860
CMD ["./app"]
