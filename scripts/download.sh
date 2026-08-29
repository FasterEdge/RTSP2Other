#!/usr/bin/env bash
# =====================================================================
# 下载静态 ffmpeg / ffprobe / mediamtx 到 bin/<os>-<arch>/
# 用法:
#   ./scripts/download.sh                 # 下载当前平台
#   ./scripts/download.sh linux-amd64     # 指定平台: linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64 | windows-amd64
#   ./scripts/download.sh all             # 全部
# 环境变量:
#   MTX_VERSION=v1.20.1                    # mediamtx 版本
#   FFMPEG_LINUX_URL=...                   # linux ffmpeg 下载地址(默认 johnvansickle.com)
#   FFMPEG_DARWIN_URL=...                  # darwin ffmpeg 下载地址(默认 osxexperts.net)
# 说明:
#   - 本仓库已内置全部 4 个平台的二进制, 此脚本仅用于重新获取/升级。
#   - 若 GitHub 被墙导致 mediamtx 下载失败, 可改用源码构建(需 Go 1.22+):
#       git clone https://github.com/bluenviron/mediamtx && cd mediamtx \
#         && GOOS=<os> GOARCH=<arch> CGO_ENABLED=0 go build -o <dest>/mediamtx .
# =====================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MTX_VERSION="${MTX_VERSION:-v1.20.1}"
FFMPEG_LINUX_URL="${FFMPEG_LINUX_URL:-https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-{arch}-static.tar.xz}"
# osxexperts 提供两种 macOS 静态构建: 8.0(intel) / 9.0(arm)
FFMPEG_DARWIN_AMD64_URL="${FFMPEG_DARWIN_AMD64_URL:-https://www.osxexperts.net/ffmpeg80intel.zip}"
FFPROBE_DARWIN_AMD64_URL="${FFPROBE_DARWIN_AMD64_URL:-https://www.osxexperts.net/ffprobe80intel.zip}"
FFMPEG_DARWIN_ARM64_URL="${FFMPEG_DARWIN_ARM64_URL:-https://www.osxexperts.net/ffmpeg9arm.zip}"
FFPROBE_DARWIN_ARM64_URL="${FFPROBE_DARWIN_ARM64_URL:-https://www.osxexperts.net/ffprobe9arm.zip}"

os_arch() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    mingw*|msys*|cygwin*) os=windows;;
  esac
  arch="$(uname -m)"
  case "$arch" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; esac
  echo "${os}-${arch}"
}

HERE="$(os_arch)"

download_mediamtx() { # $1=os $2=arch $3=dest
  local os="$1" arch="$2" dest="$3" url
  url="https://github.com/bluenviron/mediamtx/releases/download/${MTX_VERSION}/mediamtx_${MTX_VERSION}_${os}_${arch}.tar.gz"
  echo "==> mediamtx (${os}-${arch})"
  # 重试 3 次(GitHub 网络不稳)
  local n
  for n in 1 2 3; do
    if curl -fsSL --retry 2 --max-time 180 "$url" -o /tmp/rtsp2other-mtx.tar.gz; then
      tar -xzf /tmp/rtsp2other-mtx.tar.gz -C "$dest" mediamtx
      chmod +x "$dest/mediamtx"
      echo "==> mediamtx 完成: $dest/mediamtx"
      return 0
    fi
    echo "    (第 ${n} 次失败, 重试...)" >&2
    sleep 2
  done
  echo "!! mediamtx 下载失败。请检查网络, 或用源码构建(见脚本头部说明)。" >&2
  return 1
}

download_linux() { # $1 = arch(amd64|arm64)
  local arch="$1" dest="$ROOT/bin/linux-$arch"
  mkdir -p "$dest"
  local url="${FFMPEG_LINUX_URL/\{arch\}/$arch}"
  echo "==> ffmpeg/ffprobe (linux-$arch)"
  curl -fsSL --retry 2 --max-time 300 "$url" -o /tmp/rtsp2other-ffmpeg.tar.xz
  tar -xJf /tmp/rtsp2other-ffmpeg.tar.xz -C "$dest" --strip-components=1 --wildcards '*/ffmpeg' '*/ffprobe'
  chmod +x "$dest"/ffmpeg "$dest"/ffprobe
  download_mediamtx linux "$arch" "$dest"
  echo "==> 完成: $dest"
}

download_darwin() { # $1 = arch(amd64|arm64)
  local arch="$1" dest="$ROOT/bin/darwin-$arch"
  mkdir -p "$dest"
  local ff_url ffp_url
  if [ "$arch" = "arm64" ]; then
    ff_url="$FFMPEG_DARWIN_ARM64_URL"; ffp_url="$FFPROBE_DARWIN_ARM64_URL"
  else
    ff_url="$FFMPEG_DARWIN_AMD64_URL"; ffp_url="$FFPROBE_DARWIN_AMD64_URL"
  fi
  echo "==> ffmpeg (darwin-$arch, 来自 osxexperts.net)"
  curl -fsSL --retry 2 --max-time 180 "$ff_url" -o /tmp/rtsp2other-ffmpeg.zip
  unzip -o -q /tmp/rtsp2other-ffmpeg.zip -d /tmp/rtsp2other-darwin-ff ffmpeg
  mv /tmp/rtsp2other-darwin-ff/ffmpeg "$dest/ffmpeg"
  echo "==> ffprobe (darwin-$arch)"
  curl -fsSL --retry 2 --max-time 180 "$ffp_url" -o /tmp/rtsp2other-ffprobe.zip
  unzip -o -q /tmp/rtsp2other-ffprobe.zip -d /tmp/rtsp2other-darwin-ff ffprobe
  mv /tmp/rtsp2other-darwin-ff/ffprobe "$dest/ffprobe"
  chmod +x "$dest"/ffmpeg "$dest"/ffprobe
  download_mediamtx darwin "$arch" "$dest"
  echo "==> 完成: $dest"
}

download_windows() { # $1 = arch(amd64)
  local arch="$1" dest="$ROOT/bin/windows-$arch"
  mkdir -p "$dest"
  echo "==> ffmpeg/ffprobe (windows-$arch, 来自 gyan.dev)"
  curl -fsSL --retry 2 --max-time 400 "https://www.gyan.dev/ffmpeg/builds/ffmpeg-release-essentials.zip" -o /tmp/rtsp2other-ffmpeg.zip
  unzip -o -q /tmp/rtsp2other-ffmpeg.zip -d /tmp/rtsp2other-win-ff
  local bin="$(dirname "$(find /tmp/rtsp2other-win-ff -name ffmpeg.exe | head -1)")"
  mv "$bin/ffmpeg.exe" "$bin/ffprobe.exe" "$dest/"
  chmod +x "$dest"/*.exe
  download_mediamtx windows "$arch" "$dest"
  echo "==> 完成: $dest"
}

target="${1:-$HERE}"
case "$target" in
  all)
    download_linux amd64; download_linux arm64; download_darwin amd64; download_darwin arm64; download_windows amd64 ;;
  linux-amd64) download_linux amd64 ;;
  linux-arm64) download_linux arm64 ;;
  darwin-amd64) download_darwin amd64 ;;
  darwin-arm64) download_darwin arm64 ;;
  windows-amd64) download_windows amd64 ;;
  *) echo "未知平台: $target"; echo "支持: linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64 | windows-amd64 | all"; exit 1 ;;
esac
