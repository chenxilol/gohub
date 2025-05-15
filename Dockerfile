FROM golang:1.22-alpine AS builder

WORKDIR /app

# 复制go模块定义和依赖
COPY ../go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY .. .

# 构建可执行文件
RUN CGO_ENABLED=0 GOOS=linux go build -o gohub ./cmd/gohub

# 创建最终镜像
FROM alpine:latest

WORKDIR /app

# 添加必要的CA证书
RUN apk --no-cache add ca-certificates

# 从构建阶段复制可执行文件和配置
COPY --from=builder /app/gohub /app/
COPY --from=builder /app/configs /app/configs

EXPOSE 8080

CMD ["/app/gohub"] 