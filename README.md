# RTSP2Other

用 **Go** 管理 **ffmpeg 子进程**, 把一路 RTSP 视频流稳定地转换成**多路**其它格式/协议输出的工具:

| 输出类型 | 说明 |
|---|---|
| `stdout` | 把媒体流写到进程 stdout, 可与 `ffplay` / 其它程序管道对接 |
| `http_mjpg` | 本地 HTTP **MJPEG** 流 (`multipart/x-mixed-replace`), 浏览器 `<img>` / VLC 直接看, 支持多客户端 |
| `rtsp` | 本地 **RTSP** 输出: `push`(推流到内置 mediamtx, 多客户端) / `listen`(ffmpeg 内置服务端, 单客户端) |
| `mp4` | 分段式(fragmented)**MP4 流式文件**, 边写边可经 HTTP 范围请求播放, 可选按时长滚动分片 |
| `hls` | **HLS 直播流**(fMP4 切片), 网页 / 移动端低延迟播放 |

- **一个输入, 任意多路输出**: 每个输出独立一个 ffmpeg 子进程, 独立转码/复制、独立重连/重启, 互不影响。
- **自动守护**: 输出进程崩溃/卡死自动重启(指数退避); 无进展守护(watchdog)自动恢复挂死的输入连接。
- **优雅退出**: `Ctrl+C` / `SIGTERM` 会先让 ffmpeg 正常收尾(mp4 等文件保持可播放), 再清理子进程。
- **完整配置**: YAML 配置, 覆盖输入、转码参数(码率/CRF/帧率/分辨率/编码器/硬件加速)、音频、各类输出专有参数。
- **附带二进制**: 仓库内置静态 `ffmpeg` / `ffprobe` / `mediamtx`(linux-amd64, 用于 Docker; 也可下载本机平台), 单文件均 < 100MB。
- **开箱即用**: 提供 `Dockerfile` / `docker-compose.yml` / `Makefile` / 下载脚本。

---

## 架构

```
                    ┌────────────┐
  RTSP 摄像机 ─────▶│  引擎(Go)  │── 守护/重启/看门狗 ──▶ ffmpeg 子进程 #1 ──▶ stdout (mpegts)
                    │            │── 守护/重启/看门狗 ──▶ ffmpeg 子进程 #2 ──▶ HTTP MJPG(广播多客户端)
                    │            │── 守护/重启/看门狗 ──▶ ffmpeg 子进程 #3 ──▶ RTSP push ─▶ mediamtx ─▶ 观看端
                    │            │── 守护/重启/看门狗 ──▶ ffmpeg 子进程 #4 ──▶ MP4 流式文件
                    │            │── 守护/重启/看门狗 ──▶ ffmpeg 子进程 #5 ──▶ HLS 切片
                    └─────┬──────┘
                          │ 内置 HTTP 服务(:8080) —— 状态页 / /status.json / MJPG / HLS / MP4 播放页
```

日志一律走 **stderr**, 因此 `stdout` 输出类型下 stdout 是干净的媒体流。

---

## 快速开始

### 1. 准备二进制

```bash
# 方式 A: 使用仓库已内置的(linux-amd64)或执行脚本下载本机平台
./scripts/download.sh              # 自动下载当前平台(linux-amd64/arm64, darwin-amd64/arm64)的 ffmpeg + mediamtx

# 方式 B: 使用系统已安装的 ffmpeg(如 macOS: brew install ffmpeg), mediamtx 可选
#   - 工具会自动按优先级查找: 配置 path > 环境变量 > bin/<os>-<arch>/ > PATH
```

### 2. 修改配置

复制 `example-config.yaml`, 把 `input.url` 改成你的 RTSP 地址, 按需增删 `outputs`。

```bash
./rtsp2other -config my-config.yaml -check   # 先校验配置, 打印摘要
./rtsp2other -config my-config.yaml          # 运行
```

### 3. 查看

- 状态页 / 播放页: <http://127.0.0.1:8080/>
- 状态 JSON: <http://127.0.0.1:8080/status.json>
- 各流:
  - MJPEG: `http://127.0.0.1:8080/streams/<输出名>.mjpg`
  - HLS: `http://127.0.0.1:8080/streams/<输出名>-hls/index.m3u8`
  - MP4: `http://127.0.0.1:8080/streams/<输出名>.mp4`
  - RTSP: `rtsp://<本机IP>:8554/<输出名>` (VLC / ffplay 打开)

### Docker

```bash
make docker                                  # 或 docker build -t rtsp2other .

docker run --rm \
  -v $PWD/my-config.yaml:/app/rtsp2other.yaml:ro \
  -p 8080:8080 -p 8554:8554 \
  rtsp2other

# 或 docker compose up -d (已带 docker-compose.yml)
```

镜像在构建阶段自动下载静态 ffmpeg + mediamtx, 无需本地二进制; 运行阶段二进制已在镜像内。

### 一次性任务 / 脚本

```bash
./rtsp2other -config x.yaml -once     # 任一输出进程退出后停止全部
./rtsp2other -config x.yaml -check    # 只校验配置并退出
./rtsp2other -version
```

---

## 命令行参数

| 参数 | 默认 | 说明 |
|---|---|---|
| `-config` | `rtsp2other.yaml` | 配置文件路径 |
| `-once` | `false` | 任一输出进程退出后停止全部(适合脚本/一次性任务) |
| `-check` | `false` | 只校验配置、打印摘要并退出(无需二进制) |
| `-version` | - | 打印版本 |
| `-log` | `info` | 日志级别 `debug|info|warn|error` |

环境变量: `RTSP2OTHER_FFMPEG` / `RTSP2OTHER_MTX` / `RTSP2OTHER_CONFIG`(已由 `-config` 取代)。

---

## 配置说明

完整示例见 [`example-config.yaml`](example-config.yaml), 所有可选项都有注释。下面按模块说明关键点。

### input(输入)

- `url`: 任意 ffmpeg 可读的输入, 通常为 `rtsp://user:pass@host:554/stream`。
- `transport`: `tcp|udp`, 默认 `tcp`(更稳定)。
- `reconnect`: ffmpeg 层自动重连(http/file 协议有效)。
- `socket_timeout_ms`: 注入 `-rw_timeout` 实现 socket 超时。**注意**: Homebrew 版 ffmpeg 不支持该选项(会启动失败), 请保持 0; 仓库内置静态构建与 Docker 镜像支持, 建议 5000。进程级兜底由 `watchdog_timeout` 提供。
- `extra_args`: 追加在 `-i` 之前的原始参数。

### ffmpeg

- `path`: 二进制路径; 留空自动查找(配置 > 环境变量 > `bin/<os>-<arch>/` > 可执行文件同目录 > PATH)。
- `probe`: 启动时用 ffprobe 打印输入流摘要。
- `log_level`: ffmpeg stderr 级别, 默认 `warning`。
- `hardware_accel`: 硬件编码器名(如 `h264_nvenc`), 非空时 video transcode 优先使用。
- `restart_delay` / `max_restart_delay`: 崩溃重启的初始/上限延迟(指数退避, 稳定运行 30s 后退避重置)。
- `watchdog_timeout`: **无进展守护**(默认 30s, 0=关闭)。通过 `ffmpeg -progress` 判断是否仍在出帧, 超时则自动重启该输出——能自动恢复"连接挂着但不通"的场景, 兼容任意 ffmpeg 构建。

### http

- `enabled` / `listen`(默认 `0.0.0.0:8080`) / `prefix`(反代前缀)。所有 HTTP 输出共享一个端口。

### mediamtx(rtsp push 模式所需)

- `path` / `listen`(默认 `0.0.0.0:8554`) / `auth`(`user:pass`, 可选) / `log_level`。
- 工具自动为 mediamtx 生成最小配置(仅开启 RTSP, 关闭其它服务, 避免端口冲突)。

### outputs(输出, 可多个)

每个输出独立一个 ffmpeg 子进程。公共字段:

- `name`: 输出名(唯一), 用于日志与 URL。
- `type`: `stdout|http_mjpg|rtsp|mp4|hls`。
- `enabled` / `restart` / `restart_delay` / `max_restart_delay`。
- `input`: 可选, 覆盖全局输入。
- `video` / `audio`: 见下。

**video(公共)**:

| 字段 | 说明 |
|---|---|
| `mode` | `copy`(不转码直接复制) / `transcode`(重新编码) |
| `codec` | `h264|h265|mjpeg|mpeg4|vp8|vp9|av1` 或任意 ffmpeg 编码器名(如 `h264_nvenc`) |
| `bitrate` / `maxrate` / `bufsize` | 码率控制, 如 `2M` |
| `crf` | 恒定质量 0~51(设了 crf 就不再设 bitrate) |
| `preset` / `tune` / `profile` / `level` | libx264/x265 参数(低延迟用 `tune: zerolatency`) |
| `fps` | 输出帧率 |
| `scale` | 缩放, 如 `1280x720`(与 fps 同时设置会生成 `scale,fps` 滤镜链) |
| `gop` / `pix_fmt` | 关键帧间隔 / 像素格式 |
| `extra_args` | 追加的额外视频参数 |

**audio(公共)**: `mode: copy|transcode|disable`; transcode 时 `codec`(默认 aac) / `bitrate` / `sample_rate` / `channels` / `extra_args`。

**各类型专有**:

| 类型 | 专有字段 | 默认行为 |
|---|---|---|
| `stdout` | `format: mpegts\|matroska\|flv\|h264\|hevc\|mp4` | 默认 mpegts, video/audio 默认 copy; **最多一个 stdout 输出** |
| `http_mjpg` | `quality: 2~5`(越小越清晰) | 强制转码 mjpeg, 默认 15fps, 无音频 |
| `rtsp` | `mode: push\|listen`; `target` | push 默认推到 `rtsp://127.0.0.1:8554/<name>`; listen 默认 `rtsp://0.0.0.0:8555/<name>` |
| `mp4` | `path`; `rotation`(如 `30s` 按时长滚动) | 分段式 mp4(frag_keyframe+empty_moov), 支持边写边播放 |
| `hls` | `dir`; `segment_time`; `segment_list_size`; `playlist_type: live\|event\|vod`; `segment_type: fmp4\|mpegts` | fmp4 切片, live 滚动窗口 |

---

## 输出类型详解

### stdout
`ffmpeg -i <input> ... -f mpegts pipe:1`, 流写到本进程 stdout。与管道配合:
```bash
./rtsp2other -config a.yaml | ffplay -
```
注意日志都在 stderr, 不会污染媒体流。

### http_mjpg
ffmpeg 以 MJPEG 编码输出到管道, Go 侧按 JPEG 帧边界(SOI/EOI)切分, 以 `multipart/x-mixed-replace` 广播给**任意数量**客户端(慢客户端自动丢帧)。用 `-q:v` 与 `fps`/`scale` 控制带宽。

### rtsp
- **push**(推荐): 工具自动启动 mediamtx(仅开 RTSP), ffmpeg 把转码后的流推上去, 任意客户端 `rtsp://IP:8554/<name>` 观看。多实例/多路不冲突。
- **listen**: 直接用 ffmpeg 内置 RTSP 服务端, 无需 mediamtx; 官方限制**单客户端**。注意部分 ffmpeg 构建(如 Homebrew on macOS)不支持输出 listen 模式, 请优先用 push。

### mp4
默认单文件分段式 mp4(`-movflags +frag_keyframe+empty_moov`), 文件从头就是可流式播放的, HTTP 范围请求支持边写边看。`rotation` 设为 `30s` 等可按时长滚动生成 `name_20260829_103000.mp4` 文件。

### hls
fMP4 切片 + 滚动窗口直播(默认保留 6 个切片), 通过内置 HTTP 静态服务提供; 浏览器用 hls.js(播放页已内置)或 Safari 原生播放。

---

## 运行机制 / 可靠性

1. **每输出一个进程**: 一个输出崩溃不影响其它输出; 输入断流时每个输出独立重连。
2. **指数退避重启**: 初始 `restart_delay`(默认 1s), 每次失败翻倍, 上限 `max_restart_delay`(默认 30s); 稳定运行 ≥30s 后退避重置, 避免频繁重启风暴。
3. **无进展守护(watchdog)**: 通过 `ffmpeg -progress` 实时判断是否仍在出帧; 输入连接挂死但进程没退出时, 超时自动 SIGTERM→SIGKILL 重启。
4. **优雅退出**: 收到信号后先 SIGTERM 让 ffmpeg 收尾(写全 moov/切片), 宽限期 6s 后强制 SIGKILL, 再关闭 HTTP/mediamtx。
5. **可观测**: `/status.json` 给出每个输出的状态、PID、重启次数、上次退出原因、运行时长、额外信息(如 mjpg 在线客户端数)。

---

## 目录结构

```
.
├── main.go                     # 入口: 参数/信号/装配
├── example-config.yaml         # 完整配置示例
├── internal/
│   ├── config/                 # 配置解析、默认值、校验、二进制定位
│   ├── output/                 # 每种输出的 ffmpeg 参数生成 + Runner
│   ├── mjpg/                   # MJPEG 帧切分 + 广播 Hub
│   ├── engine/                 # 子进程生命周期、重启退避、watchdog、ffprobe
│   ├── mtx/                    # mediamtx 配置生成与守护
│   └── httpserv/               # 状态页 / 播放页 / 流路由
├── bin/<os>-<arch>/            # 内置静态 ffmpeg/ffprobe/mediamtx
├── scripts/download.sh         # 下载二进制脚本
├── Dockerfile / docker-compose.yml
└── Makefile
```

## 构建

```bash
go build ./...        # 编译
make test             # 单元测试
make docker           # Docker 镜像
```

---

## 常见问题

**Q: 找不到 ffmpeg / mediamtx**
运行 `./scripts/download.sh` 下载, 或用环境变量 `RTSP2OTHER_FFMPEG` / `RTSP2OTHER_MTX` 指向二进制。

**Q: 推流到 8554 失败 / 端口冲突**
mediamtx 只开 RTSP; 若你另外起了 mediamtx, 请修改 `mediamtx.listen` 与对应输出的 `target`。

**Q: Homebrew 版 ffmpeg 一些参数不支持**
Homebrew on macOS 构建较特殊(不支持 `-rw_timeout`, 部分 RTSP listen 场景), 建议在 macOS 上用仓库脚本下载的 ffmpeg 或改用 Docker。

**Q: 为什么每个输出单独一个 ffmpeg 进程**
为了输出之间彻底隔离: 一个输出(如 HLS)的故障与重启不影响其它输出。代价是多个 transcode 会多占 CPU; 对多数场景(多路监控)可接受, 如需共享解码可自行调整。

**Q: mjpg 很卡**
降低 `fps` / `scale` / 调大 `quality`; mjpg 是 MJPEG 逐帧传输, 高帧率高分辨率很占带宽。

---

## License

MIT
