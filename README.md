<div align="center">
  <img src="Logo.png" alt="RTSP2Other" width="100" />
  <h2>RTSP2Other</h2>
  <h3>基于 Go 管理 ffmpeg 子进程的多路 RTSP 视频流转换工具</h3>
</div>

### 一、项目简介

- 用 **Go** 统一管理 **ffmpeg 子进程**,把一路 RTSP 摄像机流稳定地转换成**任意多路**其它格式/协议输出,各输出互不影响。
- 每个输出独立一个 ffmpeg 子进程,独立转码/复制、独立崩溃重启与无进展守护,一路输入断流不会拖垮其它输出。
- 内置静态 **ffmpeg / ffprobe / mediamtx**,覆盖 **5 大平台**(linux/darwin/windows × amd64/arm64),下载即用,无需自行安装 ffmpeg。
- 提供完整 YAML 配置(输入、转码、音频、各输出专有参数)、内置 HTTP 状态页与播放页、Dockerfile / docker-compose / Makefile。

### 二、主要特性

| 输出类型 | 说明 |
|----------|------|
| `stdout` | 媒体流写入进程 stdout,可与 `ffplay` / 其它程序管道对接(mpegts/matroska/flv/h264/hevc/mp4) |
| `http_mjpg` | 本地 HTTP **MJPEG 流**(`multipart/x-mixed-replace`),浏览器 `<img>` / VLC 直接看,支持多客户端、慢客户端自动丢帧 |
| `rtsp` | 本地 **RTSP 输出**:`push`(推流到内置 mediamtx,多客户端) / `listen`(ffmpeg 内置服务端,单客户端) |
| `mp4` | 分段式(fragmented)**MP4 流式文件**,边写边可经 HTTP 范围请求播放,可选按时长滚动分片 |
| `hls` | **HLS 直播流**(fMP4 切片),网页 / 移动端低延迟播放 |

- **自动守护**:输出进程崩溃自动重启(指数退避);无进展 watchdog 自动恢复挂死的输入连接。
- **优雅退出**:`Ctrl+C` / `SIGTERM` 先让 ffmpeg 正常收尾(mp4 等文件保持可播放),再清理子进程。
- **开箱即用**:内置多平台静态二进制、Docker 镜像(按目标架构自动选用 linux 版 ffmpeg)、状态页 `/status.json`。

### 三、快速开始

```bash
# 1. 二进制: 仓库已内置 bin/<os>-<arch>/, 或执行脚本下载本机平台
./scripts/download.sh

# 2. 复制示例配置, 把 input.url 改成你的 RTSP 地址
cp example-config.yaml rtsp2other.yaml
#   按需增删 outputs 段落(见"四、配置说明")

# 3. 校验配置并运行
./rtsp2other -config rtsp2other.yaml -check
./rtsp2other -config rtsp2other.yaml
```

浏览器访问:

- 状态页 / 播放页: <http://127.0.0.1:8080/>
- 状态 JSON: <http://127.0.0.1:8080/status.json>
- MJPEG: `http://127.0.0.1:8080/streams/<输出名>.mjpg`
- HLS: `http://127.0.0.1:8080/streams/<输出名>-hls/index.m3u8`
- MP4: `http://127.0.0.1:8080/streams/<输出名>.mp4`
- RTSP: `rtsp://<本机IP>:8554/<输出名>`(VLC / ffplay 打开)

> 命令行参数:`-config`(配置文件,默认 `rtsp2other.yaml`)、`-once`(任一输出退出即停止全部)、`-check`(仅校验配置)、`-version`、`-log`(日志级别)。
> 环境变量:`RTSP2OTHER_FFMPEG` / `RTSP2OTHER_MTX` 可显式指定二进制路径。

### 四、配置说明

完整可选项见 [`example-config.yaml`](example-config.yaml)(全部带中文注释),这里说明模块要点。

**input(输入)**

- `url`:ffmpeg 可读的任意输入,通常 `rtsp://user:pass@host:554/stream`。
- `transport`: `tcp|udp`,默认 `tcp`。
- `reconnect`: ffmpeg 层自动重连(http/file 协议有效)。
- `socket_timeout_ms`: 通过 `-rw_timeout` 注入 socket 超时。**注意**: Homebrew 版 ffmpeg 不支持该选项,请保持 0;进程级兜底由 `watchdog_timeout` 提供。
- `extra_args`: 追加在 `-i` 之前的原始参数。

**ffmpeg**

- `path`: 二进制路径;留空自动查找: 配置 > 环境变量 > `bin/<os>-<arch>/` > 可执行文件同目录 > PATH。
- `probe` / `log_level` / `global_args` / `hardware_accel`(如 `h264_nvenc`、`h264_videotoolbox`)。
- `restart_delay` / `max_restart_delay`: 崩溃重启初始/上限延迟(指数退避,稳定 30s 后重置)。
- `watchdog_timeout`: **无进展守护**(默认 30s,0=关闭),通过 `ffmpeg -progress` 判断是否仍在出帧,超时自动重启,兼容任意 ffmpeg 构建。Windows 因进程模型限制自动禁用。

**http**

- `enabled` / `listen`(默认 `0.0.0.0:8080`) / `prefix`(反代前缀),所有 HTTP 输出共享一个端口。

**mediamtx(rtsp push 模式所需)**

- `path` / `listen`(默认 `0.0.0.0:8554`) / `auth`(`user:pass`,可选) / `log_level`;自动生成仅开 RTSP 的最小配置,避免端口冲突。

**outputs(输出,可多个)**

公共字段: `name`(唯一,用于日志与 URL)、`type`、`enabled` / `restart` / `restart_delay` / `max_restart_delay`、`input`(可选覆盖)、`video`、`audio`。

| 字段 | 说明 |
|------|------|
| `video.mode` | `copy`(不转码) / `transcode`(重新编码) |
| `video.codec` | `h264|h265|mjpeg|mpeg4|vp8|vp9|av1` 或任意 ffmpeg 编码器名 |
| `video.bitrate` / `maxrate` / `bufsize` | 码率控制,如 `2M`;设 `crf` 则用恒定质量 |
| `video.preset` / `tune` / `profile` / `level` | libx264/x265 参数(低延迟用 `tune: zerolatency`) |
| `video.fps` / `scale` / `gop` / `pix_fmt` | 帧率 / 分辨率(与 fps 组合成 `scale,fps` 滤镜) / 关键帧间隔 / 像素格式 |
| `video.extra_args` | 追加的额外视频参数 |
| `audio.mode` | `copy|transcode|disable`;transcode 时 `codec`(默认 aac) / `bitrate` / `sample_rate` / `channels` / `extra_args` |

各类型专有字段:

| 类型 | 专有字段 | 默认行为 |
|------|----------|----------|
| `stdout` | `format: mpegts\|matroska\|flv\|h264\|hevc\|mp4` | 默认 mpegts 复制;**最多一个 stdout 输出** |
| `http_mjpg` | `quality: 2~5`(越小越清晰) | 强制转码 mjpeg,默认 15fps,无音频 |
| `rtsp` | `mode: push\|listen`;`target` | push 默认 `rtsp://127.0.0.1:8554/<name>`;listen 默认 `rtsp://0.0.0.0:8555/<name>` |
| `mp4` | `path`;`rotation`(如 `30s` 按时长滚动) | 分段式 mp4(frag_keyframe+empty_moov),边写边播,自动覆盖旧文件 |
| `hls` | `dir`;`segment_time`;`segment_list_size`;`playlist_type: live\|event\|vod`;`segment_type: fmp4\|mpegts` | fmp4 切片,live 滚动窗口 |

### 五、输出类型详解

**stdout**

`ffmpeg ... -f mpegts pipe:1`,流写到本进程 stdout;日志一律走 stderr,不污染媒体流:

```bash
./rtsp2other -config a.yaml | ffplay -
```

**http_mjpg**

ffmpeg 以 MJPEG 编码输出到管道,Go 侧按 JPEG 帧边界(SOI/EOI)切分,以 `multipart/x-mixed-replace` 广播给任意数量客户端,慢客户端自动丢帧。用 `-q:v` 与 `fps`/`scale` 控制带宽。

**rtsp**

- **push**(推荐):自动启动 mediamtx(仅开 RTSP),ffmpeg 推流上去,任意客户端 `rtsp://IP:8554/<name>` 观看,多实例/多路不冲突。
- **listen**:直接用 ffmpeg 内置 RTSP 服务端,无需 mediamtx;官方限制**单客户端**。部分 ffmpeg 构建(如 Homebrew on macOS)不支持输出 listen 模式,优先用 push。

**mp4**

单文件分段式 mp4(`-movflags +frag_keyframe+empty_moov`),文件从头可流式播放,HTTP 范围请求支持边写边看;`rotation` 设为 `30s` 等可按时长滚动生成 `name_20260829_103000.mp4`。

**hls**

fMP4 切片 + 滚动窗口直播(默认保留 6 个切片),内置 HTTP 静态服务提供,浏览器用 hls.js(播放页已内置)或 Safari 原生播放。

### 六、运行机制与可靠性

1. **每输出一个进程**:一个输出崩溃不影响其它输出;输入断流时每个输出独立重连。
2. **指数退避重启**:初始 `restart_delay`(默认 1s),失败翻倍至 `max_restart_delay`(默认 30s);稳定运行 ≥30s 后退避重置,避免重启风暴。
3. **无进展守护(watchdog)**:通过 `ffmpeg -progress` 实时判断是否仍在出帧;输入连接挂死时超时自动 SIGTERM→SIGKILL 重启。
4. **优雅退出**:收到信号后先 SIGTERM 让 ffmpeg 收尾(写全 moov/切片),宽限 6s 后强制 SIGKILL,再关闭 HTTP/mediamtx。
5. **可观测**:`/status.json` 输出每个输出的状态、PID、重启次数、上次退出原因、运行时长、额外信息(如 mjpg 在线客户端数)。

### 七、构建与测试

```bash
make build     # 构建本地二进制
make test      # 单元测试
make check     # 校验示例配置
make docker    # 构建 Docker 镜像(镜像内 ffmpeg 取自 bin/linux-<arch>/, 无需联网下载)
make bin       # 下载/更新本机平台二进制
```

Docker 镜像内的 ffmpeg / ffprobe / mediamtx 直接取自仓库内置的**静态二进制**(`bin/linux-<arch>/`,由 `TARGETARCH` 自动选择与镜像目标架构一致的版本),构建过程无需联网下载 ffmpeg,镜像内容与仓库版本完全一致:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t rtsp2other .
```

### 八、常见问题

**Q: 找不到 ffmpeg / mediamtx**
运行 `./scripts/download.sh`,或用环境变量 `RTSP2OTHER_FFMPEG` / `RTSP2OTHER_MTX` 指向二进制。

**Q: 推流到 8554 失败 / 端口冲突**
mediamtx 只开 RTSP;若你另外起了 mediamtx,请修改 `mediamtx.listen` 与对应输出的 `target`。

**Q: Homebrew 版 ffmpeg 一些参数不支持**
Homebrew on macOS 构建较特殊(不支持 `-rw_timeout`,部分 RTSP listen 场景),建议用仓库脚本下载的 ffmpeg 或改用 Docker。

**Q: Windows 上能跑吗**
可以。仓库已内置 `bin/windows-amd64/` 的 ffmpeg.exe/ffprobe.exe/mediamtx.exe,直接运行 `rtsp2other.exe -config rtsp2other.yaml`。受 Windows 进程模型限制:无进展看门狗自动禁用(有日志提示),优雅退出走 SIGKILL 兜底;其余功能完全一致。

**Q: 为什么每个输出单独一个 ffmpeg 进程**
为了输出之间彻底隔离:一个输出(如 HLS)的故障与重启不影响其它输出。代价是多个 transcode 会多占 CPU;对多数场景(多路监控)可接受。

**Q: mjpg 很卡**
降低 `fps` / `scale` / 调大 `quality`;mjpg 是 MJPEG 逐帧传输,高帧率高分辨率很占带宽。

---

## License

[Apache License 2.0](LICENSE)
