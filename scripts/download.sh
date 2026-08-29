#!/usr/bin/env bash
# =====================================================================
# 下载静态 ffmpeg / ffprobe / mediamtx 到 bin/<os>-<arch>/
# 用法:
#   ./scripts/download.sh                 # 下载当前平台
#   ./scripts/download.sh linux-amd64     # 指定平台: linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64
#   ./scripts/download.sh all             # 全部
# 也可用环境变量指定版本:
#   FFMPEG_URL=... MTX_VERSION=v1.20.1 ./scripts/download.sh
# =====================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MTX_VERSION="${MTX_VERSION:-v1.20.1}"

os_arch() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  arch="$(uname -m)"
  case "$arch" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; esac
  echo "${os}-${arch}"
}

HERE="$(os_arch)"

download_linux() { # $1 = arch(amd64|arm64)
  local arch="$1" dest="$ROOT/bin/linux-$arch"
  mkdir -p "$dest"
  echo "==> ffmpeg (linux-$arch)"
  curl -fSL "${FFMPEG_URL:-https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-${arch}-static.tar.xz}" -o /tmp/rtsp2other-ffmpeg.tar.xz
  tar -xJf /tmp/rtsp2other-ffmpeg.tar.xz -C "$dest" --strip-components=1 --wildcards '*/ffmpeg' '*/ffprobe'
  echo "==> mediamtx (linux-$arch)"
  curl -fSL "https://github.com/bluenviron/mediamtx/releases/download/${MTX_VERSION}/mediamtx_${MTX_VERSION}_linux_${arch}.tar.gz" -o /tmp/rtsp2other-mtx.tar.gz
  tar -xzf /tmp/rtsp2other-mtx.tar.gz -C "$dest" mediamtx
  chmod +x "$dest"/ffmpeg "$dest"/ffprobe "$dest"/mediamtx
  echo "==> 完成: $dest"
}

download_darwin() { # $1 = arch(amd64|arm64)
  local arch="$1" dest="$ROOT/bin/darwin-$arch"
  mkdir -p "$dest"
  echo "==> ffmpeg (darwin-$arch, 来自 evermeet.cx)"
  local suffix=""
  [ "$arch" = "arm64" ] && suffix="/arm64"
  curl -fSL "https://evermeet.cx/ffmpeg/getrelease${suffix}/zip" -o /tmp/rtsp2other-ffmpeg.zip
  unzip -o /tmp/rtsp2other-ffmpeg.zip -d "$dest" ffmpeg ffprobe
  echo "==> mediamtx (darwin-$arch)"
  curl -fSL "https://github.com/bluenviron/mediamtx/releases/download/${MTX_VERSION}/mediamtx_${MTX_VERSION}_darwin_${arch}.tar.gz" -o /tmp/rtsp2other-mtx.tar.gz
  tar -xzf /tmp/rtsp2other-mtx.tar.gz -C "$dest" mediamtx
  chmod +x "$dest"/ffmpeg "$dest"/ffprobe "$dest"/mediamtx
  echo "==> 完成: $dest"
}

target="${1:-$HERE}"
case "$target" in
  all)
    download_linux amd64; download_linux arm64; download_darwin amd64; download_darwin arm64 ;;
  linux-amd64) download_linux amd64 ;;
  linux-arm64) download_linux arm64 ;;
  darwin-amd64) download_darwin amd64 ;;
  darwin-arm64) download_darwin arm64 ;;
  *) echo "未知平台: $target"; echo "支持: linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64 | all"; exit 1 ;;
esac
