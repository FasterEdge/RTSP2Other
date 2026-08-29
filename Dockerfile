# syntax=docker/dockerfile:1
# =====================================================================
# rtsp2other 镜像: 多阶段构建, 自动下载静态 ffmpeg + mediamtx
# 构建: docker build -t rtsp2other .
# 运行: docker run --rm -v $PWD/rtsp2other.yaml:/app/rtsp2other.yaml:ro \
#                  -p 8080:8080 -p 8554:8554 rtsp2other
# 注意: 本镜像在构建阶段从外部下载二进制, 需要网络。
# =====================================================================

# ---------- 基础运行镜像 ----------
FROM alpine:3.21 AS base
RUN apk add --no-cache ca-certificates tzdata

# ---------- 资产层: 下载静态 ffmpeg 与 mediamtx ----------
FROM base AS assets
ARG TARGETARCH
# johnvansickle 静态 ffmpeg(amd64/arm64)
RUN apk add --no-cache curl tar xz \
    && mkdir -p /assets \
    && curl -fsSL "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-${TARGETARCH}-static.tar.xz" -o /tmp/ffmpeg.tar.xz \
    && tar -xJf /tmp/ffmpeg.tar.xz -C /assets --strip-components=1 --wildcards "*/ffmpeg" "*/ffprobe"
# mediamtx
ARG MEDIAMTX_VERSION=v1.20.1
RUN curl -fsSL "https://github.com/bluenviron/mediamtx/releases/download/${MEDIAMTX_VERSION}/mediamtx_${MEDIAMTX_VERSION}_linux_${TARGETARCH}.tar.gz" -o /tmp/mtx.tar.gz \
    && tar -xzf /tmp/mtx.tar.gz -C /assets mediamtx

# ---------- Go 构建 ----------
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
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
