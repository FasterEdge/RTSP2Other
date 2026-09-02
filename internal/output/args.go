// FasterEdge 开源项目 - Github: https://github.com/FasterEdge - Gitee: https://gitee.com/FasterEdge
// Package output 负责把每个输出配置翻译成 ffmpeg 参数,
// 并提供 Runner 抽象(进程生命周期之外的资源, 如 MJPG Hub / HTTP 路由)。
package output

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/FasterEdge/RTSP2Other/internal/config"
)

// BuildInputArgs 根据输入配置生成 "-i url" 之前的输入参数。
func BuildInputArgs(in *config.InputConfig) []string {
	var args []string
	u := in.URL
	low := strings.ToLower(u)
	if strings.HasPrefix(low, "rtsp://") || strings.HasPrefix(low, "rtsps://") {
		if in.Transport == "udp" {
			args = append(args, "-rtsp_transport", "udp")
		} else {
			args = append(args, "-rtsp_transport", "tcp")
			if in.SocketTimeoutMS > 0 {
				// -rw_timeout 单位是微秒; 仅在 TCP 传输下有效(与 rtsp_transport tcp 一起用)
				args = append(args, "-rw_timeout", strconv.Itoa(in.SocketTimeoutMS*1000))
			}
		}
	}
	if in.Reconnect {
		args = append(args,
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_delay_max", "10",
			"-reconnect_at_eof", "1",
		)
	}
	args = append(args, in.ExtraArgs...)
	args = append(args, "-i", u)
	return args
}

// encoderName 把友好别名映射为 ffmpeg 编码器名; 无法识别的按原样使用(方便写 h264_nvenc 等)。
func encoderName(codec string, hw string) string {
	if hw != "" {
		return hw
	}
	switch strings.ToLower(codec) {
	case "h264", "libx264":
		return "libx264"
	case "h265", "hevc", "libx265":
		return "libx265"
	case "mjpeg", "mjpg":
		return "mjpeg"
	case "mpeg4":
		return "mpeg4"
	case "vp8", "libvpx":
		return "libvpx"
	case "vp9", "libvpx-vp9":
		return "libvpx-vp9"
	case "av1", "libaom-av1":
		return "libaom-av1"
	case "":
		return "libx264"
	default:
		return codec
	}
}

// BuildVideoArgs 生成视频流参数。
func BuildVideoArgs(v config.VideoConfig, hw string) ([]string, error) {
	if v.Mode == "copy" || strings.EqualFold(v.Codec, "copy") {
		return []string{"-c:v", "copy"}, nil
	}
	enc := encoderName(v.Codec, hw)
	if enc == "" {
		enc = "libx264"
	}
	args := []string{"-c:v", enc}

	switch enc {
	case "libx264", "libx265":
		if v.Preset != "" {
			args = append(args, "-preset", v.Preset)
		}
		if v.Tune != "" {
			args = append(args, "-tune", v.Tune)
		}
		if v.Profile != "" {
			args = append(args, "-profile:v", v.Profile)
		}
		if v.Level != "" {
			args = append(args, "-level:v", v.Level)
		}
	}
	if v.CRF > 0 {
		args = append(args, "-crf", strconv.Itoa(v.CRF))
	}
	if v.Bitrate != "" {
		args = append(args, "-b:v", v.Bitrate)
		if v.Maxrate != "" {
			args = append(args, "-maxrate", v.Maxrate)
		}
		if v.Bufsize != "" {
			args = append(args, "-bufsize", v.Bufsize)
		}
	}

	var filters []string
	if v.Scale != "" {
		filters = append(filters, "scale="+v.Scale)
	}
	if v.FPS > 0 && v.Scale != "" {
		filters = append(filters, "fps="+strconv.Itoa(v.FPS))
	}
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	} else if v.FPS > 0 {
		args = append(args, "-r", strconv.Itoa(v.FPS))
	}
	if v.GOP > 0 {
		args = append(args, "-g", strconv.Itoa(v.GOP))
	}
	if v.PixFmt != "" {
		args = append(args, "-pix_fmt", v.PixFmt)
	}
	args = append(args, v.ExtraArgs...)
	return args, nil
}

// BuildAudioArgs 生成音频流参数。
func BuildAudioArgs(a config.AudioConfig) []string {
	switch strings.ToLower(a.Mode) {
	case "disable":
		return []string{"-an"}
	case "copy", "":
		return []string{"-c:a", "copy"}
	default:
		codec := a.Codec
		if codec == "" {
			codec = "aac"
		}
		if strings.EqualFold(codec, "copy") {
			return []string{"-c:a", "copy"}
		}
		args := []string{"-c:a", codec}
		if a.Bitrate != "" {
			args = append(args, "-b:a", a.Bitrate)
		}
		if a.SampleRate > 0 {
			args = append(args, "-ar", strconv.Itoa(a.SampleRate))
		}
		if a.Channels > 0 {
			args = append(args, "-ac", strconv.Itoa(a.Channels))
		}
		args = append(args, a.ExtraArgs...)
		return args
	}
}

// BuildOutputHeaderArgs 返回视频+音频(及格式通用)部分参数, 不含输出 URL。
func BuildOutputHeaderArgs(oc *config.OutputConfig, hw string) ([]string, error) {
	va, err := BuildVideoArgs(oc.Video, hw)
	if err != nil {
		return nil, fmt.Errorf("output %q 视频参数: %w", oc.Name, err)
	}
	aa := BuildAudioArgs(oc.Audio)
	var args []string
	args = append(args, va...)
	args = append(args, aa...)
	return args, nil
}
