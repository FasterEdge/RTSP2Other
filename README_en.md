<div align="center">
  <img src="Logo.png" alt="RTSP2Other" width="100" />
  <h2>RTSP2Other</h2>
  <h3>A Multi-Output RTSP Stream Converter Powered by Go + ffmpeg</h3>
</div>

### 1. Introduction

- A tool that manages **ffmpeg subprocesses** with **Go** to reliably convert a single RTSP camera stream into **any number of outputs** (formats/protocols) that are fully isolated from each other.
- Each output runs its own ffmpeg subprocess with independent transcode/copy settings, independent crash-restart supervision, and a stall watchdog — one dead input stream won't take down the others.
- Ships static **ffmpeg / ffprobe / mediamtx** binaries for **5 platforms** (linux/darwin/windows × amd64/arm64) — download and run, no need to install ffmpeg yourself.
- Provides a complete YAML configuration (input, transcode, audio, per-output options), a built-in HTTP status/player page, plus Dockerfile / docker-compose / Makefile.

### 2. Key Features

| Output type | Description |
|-------------|-------------|
| `stdout` | Writes the media stream to process stdout for piping to `ffplay` / other programs (mpegts/matroska/flv/h264/hevc/mp4) |
| `http_mjpg` | Local HTTP **MJPEG stream** (`multipart/x-mixed-replace`), viewable in a browser `<img>` / VLC; multiple clients, slow clients drop frames |
| `rtsp` | Local **RTSP output**: `push` (to the built-in mediamtx, multiple clients) / `listen` (ffmpeg built-in server, single client) |
| `mp4` | Fragmented **MP4 streaming file**, playable while being written via HTTP range requests; optional time-based rotation |
| `hls` | **HLS live stream** (fMP4 segments) for low-latency playback in browsers / mobile |

- **Auto supervision**: crashed output processes restart automatically (exponential backoff); a stall watchdog recovers hung input connections.
- **Graceful shutdown**: on `Ctrl+C` / `SIGTERM` ffmpeg is allowed to finalize (mp4 files stay playable) before subprocesses are cleaned up.
- **Out of the box**: multi-platform static binaries, Docker image (automatically picks the Linux ffmpeg matching the target arch), and a `/status.json` status page.

### 3. Quick Start

```bash
# 1. Binaries: already bundled under bin/<os>-<arch>/, or download for your platform
./scripts/download.sh

# 2. Copy the example config and point input.url at your RTSP source
cp example-config.yaml rtsp2other.yaml
#   Adjust the outputs section as needed (see section 4)

# 3. Validate the config, then run
./rtsp2other -config rtsp2other.yaml -check
./rtsp2other -config rtsp2other.yaml
```

Browse to:

- Status / player page: <http://127.0.0.1:8080/>
- Status JSON: <http://127.0.0.1:8080/status.json>
- MJPEG: `http://127.0.0.1:8080/streams/<output-name>.mjpg`
- HLS: `http://127.0.0.1:8080/streams/<output-name>-hls/index.m3u8`
- MP4: `http://127.0.0.1:8080/streams/<output-name>.mp4`
- RTSP: `rtsp://<host-ip>:8554/<output-name>` (open with VLC / ffplay)

> CLI flags: `-config` (config file, default `rtsp2other.yaml`), `-once` (stop everything when any output exits), `-check` (validate only), `-version`, `-log` (log level).
> Environment: `RTSP2OTHER_FFMPEG` / `RTSP2OTHER_MTX` to explicitly point at the binaries.

### 4. Configuration

See [`example-config.yaml`](example-config.yaml) for every option (fully commented in Chinese). Highlights below.

**input**

- `url`: any input ffmpeg can read, typically `rtsp://user:pass@host:554/stream`.
- `transport`: `tcp|udp`, default `tcp`.
- `reconnect`: ffmpeg-level auto-reconnect (effective for http/file).
- `socket_timeout_ms`: injects `-rw_timeout` socket timeout. **Note**: Homebrew ffmpeg doesn't support this option — keep it 0; process-level recovery is provided by `watchdog_timeout`.
- `extra_args`: raw arguments prepended before `-i`.

**ffmpeg**

- `path`: binary path; when empty, auto-resolved as: config > env > `bin/<os>-<arch>/` > next to the executable > PATH.
- `probe` / `log_level` / `global_args` / `hardware_accel` (e.g. `h264_nvenc`, `h264_videotoolbox`).
- `restart_delay` / `max_restart_delay`: initial/max crash-restart delay (exponential backoff, reset after 30s of stability).
- `watchdog_timeout`: **stall watchdog** (default 30s, 0 to disable). Uses `ffmpeg -progress` to detect whether frames are still being produced and restarts on stall; works with any ffmpeg build. Automatically disabled on Windows.

**http**

- `enabled` / `listen` (default `0.0.0.0:8080`) / `prefix` (reverse-proxy prefix). All HTTP outputs share one port.

**mediamtx (required for rtsp push mode)**

- `path` / `listen` (default `0.0.0.0:8554`) / `auth` (`user:pass`, optional) / `log_level`. A minimal RTSP-only config is generated automatically to avoid port conflicts.

**outputs (multiple allowed)**

Common fields: `name` (unique, used in logs and URLs), `type`, `enabled` / `restart` / `restart_delay` / `max_restart_delay`, `input` (optional override), `video`, `audio`.

| Field | Description |
|-------|-------------|
| `video.mode` | `copy` (no re-encode) / `transcode` (re-encode) |
| `video.codec` | `h264\|h265\|mjpeg\|mpeg4\|vp8\|vp9\|av1` or any ffmpeg encoder name |
| `video.bitrate` / `maxrate` / `bufsize` | bitrate control, e.g. `2M`; use `crf` for constant quality |
| `video.preset` / `tune` / `profile` / `level` | libx264/x265 options (use `tune: zerolatency` for low latency) |
| `video.fps` / `scale` / `gop` / `pix_fmt` | frame rate / resolution (combined into a `scale,fps` filter) / GOP / pixel format |
| `video.extra_args` | extra video arguments |
| `audio.mode` | `copy\|transcode\|disable`; when transcode: `codec` (default aac) / `bitrate` / `sample_rate` / `channels` / `extra_args` |

Per-type fields:

| Type | Specific fields | Default behavior |
|------|-----------------|------------------|
| `stdout` | `format: mpegts\|matroska\|flv\|h264\|hevc\|mp4` | mpegts copy; **at most one stdout output** |
| `http_mjpg` | `quality: 2~5` (smaller = sharper) | forces mjpeg encode, 15 fps, no audio |
| `rtsp` | `mode: push\|listen`; `target` | push defaults to `rtsp://127.0.0.1:8554/<name>`; listen to `rtsp://0.0.0.0:8555/<name>` |
| `mp4` | `path`; `rotation` (e.g. `30s` for time-based rotation) | fragmented mp4 (`frag_keyframe+empty_moov`), playable while writing, auto-overwrites |
| `hls` | `dir`; `segment_time`; `segment_list_size`; `playlist_type: live\|event\|vod`; `segment_type: fmp4\|mpegts` | fmp4 segments, live rolling window |

### 5. Output Types in Detail

**stdout**

`ffmpeg ... -f mpegts pipe:1` — the stream goes to process stdout; all logs go to stderr so the media stream stays clean:

```bash
./rtsp2other -config a.yaml | ffplay -
```

**http_mjpg**

ffmpeg encodes MJPEG to a pipe; Go splits frames at JPEG boundaries (SOI/EOI) and broadcasts `multipart/x-mixed-replace` to any number of clients (slow clients drop frames). Control bandwidth with `-q:v` plus `fps`/`scale`.

**rtsp**

- **push** (recommended): auto-starts mediamtx (RTSP only) and pushes the transcoded stream to it; any client can watch at `rtsp://IP:8554/<name>`. Multiple instances don't conflict.
- **listen**: uses ffmpeg's built-in RTSP server — no mediamtx needed; officially **single-client**. Some ffmpeg builds (e.g. Homebrew on macOS) don't support output listen mode, so prefer push.

**mp4**

A single fragmented mp4 (`-movflags +frag_keyframe+empty_moov`) that is streamable from the start; HTTP range requests let you watch while it's being written. Set `rotation` to `30s` etc. to roll files like `name_20260829_103000.mp4`.

**hls**

fMP4 segments + a rolling live window (6 segments by default), served by the built-in HTTP static server; browsers play with hls.js (bundled in the player page) or native Safari.

### 6. How It Works

1. **One process per output**: a crash in one output never affects the others; each output reconnects independently when the input drops.
2. **Exponential backoff restart**: starts at `restart_delay` (1s), doubles up to `max_restart_delay` (30s); resets after ≥30s of stable uptime to avoid restart storms.
3. **Stall watchdog**: uses `ffmpeg -progress` to tell whether frames are still flowing; on a hung connection it restarts via SIGTERM→SIGKILL after the timeout.
4. **Graceful shutdown**: SIGTERM lets ffmpeg finalize (moov/segments written), a 6s grace period, then SIGKILL, then HTTP/mediamtx are shut down.
5. **Observability**: `/status.json` reports per-output state, PID, restart count, last exit reason, uptime, and extras (e.g. current mjpg client count).

### 7. Build & Test

```bash
make build     # build the local binary
make test      # unit tests
make check     # validate the example config
make docker    # build the Docker image (ffmpeg from bin/linux-<arch>/, no network download)
make bin       # download/refresh binaries for your platform
```

The Docker image copies the **static Linux binaries** straight from the repo (`bin/linux-<arch>/`, selected per target arch via `TARGETARCH`) — no ffmpeg download at build time, and the image content matches the repo exactly:

```bash
docker buildx build --platform linux/amd64,linux/arm64 -t rtsp2other .
```

### 8. FAQ

**Q: ffmpeg / mediamtx not found**
Run `./scripts/download.sh`, or point `RTSP2OTHER_FFMPEG` / `RTSP2OTHER_MTX` at the binaries.

**Q: Pushing to 8554 fails / port conflict**
mediamtx only opens RTSP; if you run your own mediamtx, adjust `mediamtx.listen` and the corresponding output `target`.

**Q: Homebrew ffmpeg rejects some options**
Homebrew on macOS is unusual (no `-rw_timeout`, some RTSP listen scenarios fail). Prefer the bundled ffmpeg or Docker.

**Q: Does it run on Windows?**
Yes. `bin/windows-amd64/` ships ffmpeg.exe/ffprobe.exe/mediamtx.exe; just run `rtsp2other.exe -config rtsp2other.yaml`. Due to the Windows process model, the stall watchdog is disabled (logged at startup) and graceful shutdown falls back to SIGKILL; everything else works the same.

**Q: Why one ffmpeg process per output?**
For full isolation: a failure/restart of one output (e.g. HLS) never affects the others. The cost is more CPU when transcoding; fine for most multi-camera setups.

**Q: mjpg is laggy**
Lower `fps`/`scale`, raise `quality`; MJPEG sends frame by frame, so high fps/resolution is bandwidth-hungry.

---

## License

[Apache License 2.0](LICENSE)
