# 使用多阶段构建，减小最终镜像体积
FROM golang:1.13-alpine AS builder

# 设置维护者信息
LABEL maintainer="nssvlr@gmail.com"

# 设置 Go 环境变量
ENV GOPROXY=https://goproxy.cn,https://goproxy.io,direct
ENV GO111MODULE=on
ENV CGO_ENABLED=0

WORKDIR /app

# 优化：先复制 go.mod 和 go.sum，利用 Docker 缓存
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 优化：添加构建参数，减小二进制文件大小
RUN go build -ldflags="-s -w" -o app .

# 第二阶段：使用轻量级镜像运行
FROM alpine:latest

# 安装 ca-certificates 用于 HTTPS 请求
RUN apk --no-cache add ca-certificates

WORKDIR /app

# 从 builder 阶段复制编译好的二进制文件
COPY --from=builder /app/app .

# 创建非 root 用户运行程序（安全）
RUN addgroup -g 1001 -S appgroup && \
    adduser -u 1001 -S appuser -G appgroup && \
    chown appuser:appgroup /app/app

USER appuser

# 暴露端口（根据实际情况修改）
EXPOSE 8090

CMD ["./app"]

