// Package config 定义 rtsp2other 的全部配置结构、默认值与校验逻辑。
// 配置以 YAML 为主, 支持环境变量覆盖部分运行时参数。
package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// Config 是顶层配置。
type Config struct {
	Input    InputConfig    `yaml:"input"`
	HTTP     HTTPConfig     `yaml:"http"`
	FFmpeg   FFmpegConfig   `yaml:"ffmpeg"`
	MediaMTX MediaMTXConfig `yaml:"mediamtx"`
	Outputs  []OutputConfig `yaml:"outputs"`

	// FFmpegPath / MTXPath 在 ResolveBinaries 之后被填充(二进制绝对路径)。
	FFmpegPath string `yaml:"-"`
	MTXPath    string `yaml:"-"`
}

// InputConfig 描述输入源(通常是 RTSP 摄像机)。
type InputConfig struct {
	URL             string   `yaml:"url"`               // 输入 URL, 如 rtsp://user:pass@host:554/stream
	Transport       string   `yaml:"transport"`         // rtsp 传输方式: tcp | udp, 默认 tcp
	Reconnect       bool     `yaml:"reconnect"`         // 让 ffmpeg 尝试自动重连(http/file 协议有效, rtsp 由外层守护重启兜底)
	SocketTimeoutMS int      `yaml:"socket_timeout_ms"` // rtsp socket 超时(毫秒), 默认 5000
	ExtraArgs       []string `yaml:"extra_args"`        // 追加在 -i 之前的额外输入参数
}

// HTTPConfig 控制内置 HTTP 服务(状态页 / MJPG / HLS / MP4 播放)。
type HTTPConfig struct {
	Enabled bool   `yaml:"enabled"` // 默认 true
	Listen  string `yaml:"listen"`  // 监听地址, 默认 0.0.0.0:8080
	Prefix  string `yaml:"prefix"`  // URL 前缀, 默认空; 经反向代理时可用, 如 /rtsp2other
}

// FFmpegConfig 控制 ffmpeg 二进制与全局行为。
type FFmpegConfig struct {
	Path            string   `yaml:"path"`              // ffmpeg 路径, 留空自动查找(见 ResolveBinaries)
	Probe           *bool    `yaml:"probe"`             // 启动时用 ffprobe 探测输入并打印摘要, 默认 true
	LogLevel        string   `yaml:"log_level"`         // ffmpeg stderr 日志级别: quiet|error|warning|info|debug, 默认 warning
	GlobalArgs      []string `yaml:"global_args"`       // 附加到 ffmpeg 命令最前面的全局参数, 如 [-hide_banner, -nostats]
	HardwareAccel   string   `yaml:"hardware_accel"`    // 硬件加速编码器名, 如 h264_nvenc/h264_qsv/...; 非空时视频 transcode 会优先使用
	RestartDelay    string   `yaml:"restart_delay"`     // 输出进程崩溃后的初始重启延迟, 默认 1s
	MaxRestartDelay string   `yaml:"max_restart_delay"` // 重启延迟上限, 默认 30s
	WatchdogTimeout string   `yaml:"watchdog_timeout"`  // 无进展守护: 超过该时长没有新帧则重启进程(0=关闭), 默认 30s; 依赖 ffmpeg -progress
}

// MediaMTXConfig 控制内置的 mediamtx RTSP 服务端(用于 rtsp push 输出)。
type MediaMTXConfig struct {
	Path      string   `yaml:"path"`       // mediamtx 二进制路径, 留空自动查找
	Listen    string   `yaml:"listen"`     // RTSP 监听地址, 默认 0.0.0.0:8554
	Auth      string   `yaml:"auth"`       // 可选鉴权, 格式 user:pass; 为空则不鉴权
	LogLevel  string   `yaml:"log_level"`  // error|warn|info|debug, 默认 info
	ExtraArgs []string `yaml:"extra_args"` // 透传到 mediamtx 命令的额外参数(会覆盖同名键)
}

// OutputConfig 描述一个输出管道。每个输出独立启动一个 ffmpeg 子进程,
// 独立重连/重启, 互不影响。
type OutputConfig struct {
	Name            string       `yaml:"name"`    // 输出名(唯一), 用于日志/路由
	Type            string       `yaml:"type"`    // stdout|http_mjpg|rtsp|mp4|hls
	Enabled         *bool        `yaml:"enabled"` // 默认 true
	Input           *InputConfig `yaml:"input"`   // 可选: 覆盖全局 input
	Restart         *bool        `yaml:"restart"` // 默认 true
	RestartDelay    string       `yaml:"restart_delay"`
	MaxRestartDelay string       `yaml:"max_restart_delay"`
	Format          string       `yaml:"format"` // stdout 输出容器: mpegts|matroska|flv|h264|hevc|mp4, 默认 mpegts
	Video           VideoConfig  `yaml:"video"`
	Audio           AudioConfig  `yaml:"audio"`
	ExtraArgs       []string     `yaml:"extra_args"` // 追加到输出 URL 之前的额外参数

	// ---- http_mjpg 专用 ----
	Quality int `yaml:"quality"` // MJPEG 质量 -q:v, 2~5, 越小质量越高; 默认 3

	// ---- rtsp 专用 ----
	RTSPMode string `yaml:"mode"`   // push(推流到内置 mediamtx) | listen(ffmpeg 内置 RTSP 服务端, 单客户端)
	Target   string `yaml:"target"` // push: 目标 rtsp URL; listen: rtsp://0.0.0.0:PORT/path

	// ---- mp4 专用 ----
	Path     string `yaml:"path"`     // mp4 文件路径
	Rotation string `yaml:"rotation"` // 分片时长, 如 30s; 留空为单个分段式 mp4

	// ---- hls 专用 ----
	Dir             string `yaml:"dir"`               // HLS 输出目录
	SegmentTime     int    `yaml:"segment_time"`      // 切片秒数, 默认 4
	SegmentListSize int    `yaml:"segment_list_size"` // 直播滚动窗口切片数, 默认 6
	PlaylistType    string `yaml:"playlist_type"`     // live|event|vod, 默认 live
	SegmentType     string `yaml:"segment_type"`      // fmp4|mpegts, 默认 fmp4
}

// VideoConfig 描述视频转码/复制参数。
type VideoConfig struct {
	Mode      string   `yaml:"mode"`       // copy(直接复制, 不转码) | transcode, 默认按输出类型
	Codec     string   `yaml:"codec"`      // h264|h265|mjpeg|mpeg4|vp8|vp9|av1|libx264|... 或 ffmpeg 编码器名
	Bitrate   string   `yaml:"bitrate"`    // 如 2M
	Maxrate   string   `yaml:"maxrate"`    // 如 2M(限峰)
	Bufsize   string   `yaml:"bufsize"`    // 如 4M(VBV 缓冲)
	CRF       int      `yaml:"crf"`        // 恒定质量 0~51, 默认 23
	Preset    string   `yaml:"preset"`     // libx264: ultrafast..veryslow
	Tune      string   `yaml:"tune"`       // 如 zerolatency(低延迟)
	Profile   string   `yaml:"profile"`    // 如 high
	Level     string   `yaml:"level"`      // 如 4.1
	FPS       int      `yaml:"fps"`        // 输出帧率, 0 表示不限制
	Scale     string   `yaml:"scale"`      // 缩放, 如 1280x720; 与 FPS 同时设置时生成 scale,fps 滤镜链
	PixFmt    string   `yaml:"pix_fmt"`    // 如 yuv420p
	GOP       int      `yaml:"gop"`        // 关键帧间隔(帧数), 0 表示不设置
	ExtraArgs []string `yaml:"extra_args"` // 追加的额外视频参数
}

// AudioConfig 描述音频参数。
type AudioConfig struct {
	Mode       string   `yaml:"mode"`        // copy | transcode | disable, 默认 copy
	Codec      string   `yaml:"codec"`       // aac|mp3|opus|ac3|... 或编码器名, 默认 aac
	Bitrate    string   `yaml:"bitrate"`     // 如 128k
	SampleRate int      `yaml:"sample_rate"` // 如 44100
	Channels   int      `yaml:"channels"`    // 如 2
	ExtraArgs  []string `yaml:"extra_args"`
}

// Load 从文件加载配置, 应用默认值并校验。
func Load(path string) (*Config, error) {
	cfg, err := loadOnly(path)
	if err != nil {
		return nil, err
	}
	if err := cfg.ResolveBinaries(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// LoadForCheck 仅解析+默认值+校验, 不解析二进制(供 -check 使用)。
func LoadForCheck(path string) (*Config, error) {
	return loadOnly(path)
}

func loadOnly(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
	}
	cfg.FillDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// FillDefaults 补齐所有未设置项。
func (c *Config) FillDefaults() {
	// 全局 input
	c.Input.Transport = defStr(c.Input.Transport, "tcp")
	// SocketTimeoutMS 默认 0 = 不注入 -rw_timeout(部分 ffmpeg 构建不支持该选项; 见 README)

	// http
	if !c.HTTP.Enabled && c.HTTP.Listen == "" && c.HTTP.Prefix == "" {
		c.HTTP.Enabled = true // 显式 disabled 时保留 false
	}
	if c.HTTP.Listen == "" {
		c.HTTP.Listen = "0.0.0.0:8080"
	}

	// ffmpeg
	if c.FFmpeg.Probe == nil {
		b := true
		c.FFmpeg.Probe = &b
	}
	c.FFmpeg.LogLevel = defStr(c.FFmpeg.LogLevel, "warning")
	c.FFmpeg.RestartDelay = defStr(c.FFmpeg.RestartDelay, "1s")
	c.FFmpeg.MaxRestartDelay = defStr(c.FFmpeg.MaxRestartDelay, "30s")
	c.FFmpeg.WatchdogTimeout = defStr(c.FFmpeg.WatchdogTimeout, "30s")

	// mediamtx
	c.MediaMTX.Listen = defStr(c.MediaMTX.Listen, "0.0.0.0:8554")
	c.MediaMTX.LogLevel = defStr(c.MediaMTX.LogLevel, "info")

	// 各输出
	for i := range c.Outputs {
		o := &c.Outputs[i]
		if o.Enabled == nil {
			b := true
			o.Enabled = &b
		}
		if o.Restart == nil {
			b := true
			o.Restart = &b
		}
		if o.RestartDelay == "" {
			o.RestartDelay = c.FFmpeg.RestartDelay
		}
		if o.MaxRestartDelay == "" {
			o.MaxRestartDelay = c.FFmpeg.MaxRestartDelay
		}
		o.Video.Mode = defStr(o.Video.Mode, "")
		o.Audio.Mode = defStr(o.Audio.Mode, "")
		switch o.Type {
		case "stdout":
			o.Format = defStr(o.Format, "mpegts")
			if o.Video.Mode == "" {
				o.Video.Mode = "copy"
			}
			if o.Audio.Mode == "" {
				o.Audio.Mode = "copy"
			}
		case "http_mjpg":
			if o.Video.Mode == "" {
				o.Video.Mode = "transcode"
			}
			o.Video.Codec = defStr(o.Video.Codec, "mjpeg")
			if o.Video.FPS == 0 {
				o.Video.FPS = 15
			}
			if o.Quality == 0 {
				o.Quality = 3
			}
			o.Audio.Mode = "disable"
		case "rtsp":
			if o.RTSPMode == "" {
				o.RTSPMode = "push"
			}
			if o.RTSPMode == "push" {
				if o.Video.Mode == "" {
					o.Video.Mode = "transcode"
				}
				o.Video.Codec = defStr(o.Video.Codec, "h264")
				if o.Audio.Mode == "" {
					o.Audio.Mode = "copy"
				}
				if o.Target == "" {
					o.Target = "rtsp://127.0.0.1:8554/" + o.Name
				}
			} else { // listen
				if o.Video.Mode == "" {
					o.Video.Mode = "transcode"
				}
				o.Video.Codec = defStr(o.Video.Codec, "h264")
				if o.Audio.Mode == "" {
					o.Audio.Mode = "copy"
				}
				if o.Target == "" {
					o.Target = "rtsp://0.0.0.0:8555/" + o.Name
				}
			}
		case "mp4":
			o.Path = defStr(o.Path, filepath.Join("output", "mp4", o.Name+".mp4"))
			if o.Video.Mode == "" {
				o.Video.Mode = "transcode"
			}
			o.Video.Codec = defStr(o.Video.Codec, "h264")
			if o.Audio.Mode == "" {
				o.Audio.Mode = "copy"
			}
		case "hls":
			o.Dir = defStr(o.Dir, filepath.Join("output", "hls", o.Name))
			if o.SegmentTime <= 0 {
				o.SegmentTime = 4
			}
			if o.SegmentListSize <= 0 {
				o.SegmentListSize = 6
			}
			o.PlaylistType = defStr(o.PlaylistType, "live")
			o.SegmentType = defStr(o.SegmentType, "fmp4")
			if o.Video.Mode == "" {
				o.Video.Mode = "transcode"
			}
			o.Video.Codec = defStr(o.Video.Codec, "h264")
			if o.Audio.Mode == "" {
				o.Audio.Mode = "copy"
			}
		}
	}
}

// Validate 检查配置合法性。
func (c *Config) Validate() error {
	if c.Input.URL == "" {
		return fmt.Errorf("input.url 不能为空")
	}
	if c.Input.Transport != "" && c.Input.Transport != "tcp" && c.Input.Transport != "udp" {
		return fmt.Errorf("input.transport 仅支持 tcp|udp, 得到 %q", c.Input.Transport)
	}
	if len(c.Outputs) == 0 {
		return fmt.Errorf("至少需要配置一个 outputs 项")
	}
	seen := map[string]bool{}
	stdoutCount := 0
	for i := range c.Outputs {
		o := &c.Outputs[i]
		if o.Name == "" {
			return fmt.Errorf("outputs[%d].name 不能为空", i)
		}
		if seen[o.Name] {
			return fmt.Errorf("outputs 名称重复: %q", o.Name)
		}
		seen[o.Name] = true
		if !*o.Enabled {
			continue
		}
		switch o.Type {
		case "stdout":
			stdoutCount++
			switch o.Format {
			case "mpegts", "matroska", "flv", "h264", "hevc", "mp4":
			default:
				return fmt.Errorf("output %q: 不支持的 stdout format %q", o.Name, o.Format)
			}
		case "http_mjpg":
			// 无需额外字段
		case "rtsp":
			if o.RTSPMode != "push" && o.RTSPMode != "listen" {
				return fmt.Errorf("output %q: rtsp mode 仅支持 push|listen", o.Name)
			}
			if o.Target == "" {
				return fmt.Errorf("output %q: rtsp 输出需要 target", o.Name)
			}
		case "mp4":
			if o.Path == "" {
				return fmt.Errorf("output %q: mp4 输出需要 path", o.Name)
			}
		case "hls":
			if o.Dir == "" {
				return fmt.Errorf("output %q: hls 输出需要 dir", o.Name)
			}
		default:
			return fmt.Errorf("output %q: 未知类型 %q(支持 stdout|http_mjpg|rtsp|mp4|hls)", o.Name, o.Type)
		}
		if o.Video.Mode != "" && o.Video.Mode != "copy" && o.Video.Mode != "transcode" {
			return fmt.Errorf("output %q: video.mode 仅支持 copy|transcode", o.Name)
		}
		if o.Audio.Mode != "" && o.Audio.Mode != "copy" && o.Audio.Mode != "transcode" && o.Audio.Mode != "disable" {
			return fmt.Errorf("output %q: audio.mode 仅支持 copy|transcode|disable", o.Name)
		}
		if o.Input != nil && o.Input.URL == "" {
			return fmt.Errorf("output %q: 自定义 input.url 不能为空", o.Name)
		}
	}
	if stdoutCount > 1 {
		return fmt.Errorf("最多只能有一个 stdout 输出(否则会相互覆盖 stdout)")
	}
	return nil
}

// ResolveBinaries 解析 ffmpeg / mediamtx 的二进制路径。
// 优先级: 配置 path > 环境变量 > bin/<os>-<arch>/ > PATH
func (c *Config) ResolveBinaries() error {
	ff, err := ResolveBinary("ffmpeg", c.FFmpeg.Path, env("RTSP2OTHER_FFMPEG"), "FFMPEG")
	if err != nil {
		return fmt.Errorf("无法定位 ffmpeg: %w", err)
	}
	c.FFmpegPath = ff

	// mediamtx 仅在存在 rtsp push 输出时需要
	if needsMTX(c) {
		mtx, err := ResolveBinary("mediamtx", c.MediaMTX.Path, env("RTSP2OTHER_MTX"), "MTX")
		if err != nil {
			return fmt.Errorf("存在 rtsp(push) 输出但无法定位 mediamtx: %w; 请设置 mediamtx.path 或运行 ./scripts/download.sh", err)
		}
		c.MTXPath = mtx
	}
	return nil
}

// HasRTSPPush 判断是否包含 rtsp push 输出。
func (c *Config) HasRTSPPush() bool {
	return needsMTX(c)
}

func needsMTX(c *Config) bool {
	for i := range c.Outputs {
		o := &c.Outputs[i]
		if *o.Enabled && o.Type == "rtsp" && o.RTSPMode == "push" {
			return true
		}
	}
	return false
}

func defStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func env(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

// ResolveBinary 按优先级查找二进制。
// 顺序: 配置 path > 环境变量 > bin/<os>-<arch>/ (相对 CWD 与可执行文件目录) > bin/ > PATH
// Windows 下会自动追加 .exe 后缀, 且不检查执行权限位(Windows 文件无该位)。
func ResolveBinary(name, cfgPath, envPath, envSuffix string) (string, error) {
	win := runtime.GOOS == "windows"
	exe := ""
	if win {
		exe = ".exe"
	}
	cands := []string{cfgPath, envPath}
	rel := filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, name+exe)
	cands = append(cands, rel)
	cands = append(cands, filepath.Join("bin", name+exe))
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		cands = append(cands,
			filepath.Join(exeDir, "bin", runtime.GOOS+"-"+runtime.GOARCH, name+exe),
			filepath.Join(exeDir, "bin", name+exe),
		)
	}
	for _, p := range cands {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() && (win || st.Mode()&0111 != 0) {
			abs, _ := filepath.Abs(p)
			return abs, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("未找到 %s; 请设置 RTSP2OTHER_%s / 对应配置项, 或执行 ./scripts/download.sh", name, envSuffix)
}

// OutputNames 返回所有启用的输出名。
func (c *Config) OutputNames() []string {
	var names []string
	for i := range c.Outputs {
		if *c.Outputs[i].Enabled {
			names = append(names, c.Outputs[i].Name)
		}
	}
	return names
}
