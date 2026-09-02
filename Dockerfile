# =====================================================================
# rtsp2other 镜像: 多阶段构建
# 镜像内的 ffmpeg / ffprobe / mediamtx 直接使用仓库内置的静态二进制
# (bin/linux-<arch>/, 由 TARGETARCH 自动选择, 与镜像目标架构一致),
# 构建过程不再从外部下载 ffmpeg, 保证镜像内容与仓库版本完全一致。
# 构建: docker build -t rtsp2other .
# 运行: docker run --rm -v $PWD/rtsp2other.yaml:/app/rtsp2other.yaml:ro \
#                  -p 8080:8080 -p 8554:8554 rtsp2other
# 多架构: docker buildx build --platform linux/amd64,linux/arm64 -t rtsp2other .
# =====================================================================

# ---------- 基础运行镜像 ----------
FROM alpine:3.21 AS base
RUN apk add --no-cache ca-certificates tzdata

# ---------- 资产层: 按目标架构拷贝仓库内置静态二进制 ----------
FROM base AS assets
ARG TARGETARCH
COPY bin/linux-${TARGETARCH}/ffmpeg /assets/ffmpeg
COPY bin/linux-${TARGETARCH}/ffprobe /assets/ffprobe
COPY bin/linux-${TARGETARCH}/mediamtx /assets/mediamtx

# ---------- Go 构建 ----------
FROM golang:1.24-alpine AS build
# 通过 goproxy.cn 拉取依赖, 避免在无外网代理环境(如国内网络/受限内网)构建时
# go mod download 直连 proxy.golang.org 超时。
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=1.0.20260901
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/rtsp2other .

# ---------- 最终镜像 ----------
FROM base
COPY --from=assets /assets/ffmpeg /usr/local/bin/ffmpeg
COPY --from=assets /assets/ffprobe /usr/local/bin/ffprobe
COPY --from=assets /assets/mediamtx /usr/local/bin/mediamtx
COPY --from=build /out/rtsp2other /usr/local/bin/rtsp2other

ENV RTSP2OTHER_FFMPEG=/usr/local/bin/ffmpeg \
    RTSP2OTHER_MTX=/usr/local/bin/mediamtx

WORKDIR /app
COPY example-config.yaml /app/rtsp2other.yaml

EXPOSE 8080 8554 8555

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/usr/local/bin/rtsp2other"]
CMD ["-config", "/app/rtsp2other.yaml"]