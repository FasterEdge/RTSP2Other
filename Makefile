# rtsp2other Makefile
BINARY  := rtsp2other
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS := -trimpath -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: all build bin test check vet run docker clean install

all: build

## 构建本地二进制到当前目录
build:
	go build $(GOFLAGS) -o $(BINARY) .

## 下载当前平台的 ffmpeg/mediamtx 到 bin/ (首次使用/更新二进制时执行)
bin:
	./scripts/download.sh

## 运行单元测试
test:
	go test ./...

## 校验配置文件并打印摘要
check:
	go run . -check -config example-config.yaml

vet:
	go vet ./...

## 用示例配置直接运行(需要 ffmpeg 可用, 且配置了真实 RTSP 地址)
run: build
	./$(BINARY) -config example-config.yaml

## 构建 Docker 镜像(镜像内 ffmpeg/mediamtx 取自仓库 bin/linux-<arch>/, 无需联网下载)
docker:
	docker build --build-arg VERSION=$(VERSION) -t rtsp2other:$(VERSION) .
	docker tag rtsp2other:$(VERSION) rtsp2other:latest

## 安装到 GOPATH/bin
install:
	go install $(GOFLAGS) .

clean:
	rm -f $(BINARY)
