package output

import (
	"log/slog"
	"os/exec"

	"rtsp2other/internal/config"
)

// rtspRunner 支持两种 RTSP 输出方式:
//   - push:  把流推送到内置 mediamtx(或外部)RTSP 服务端, 支持多客户端
//   - listen: 使用 ffmpeg 内置 RTSP 服务端直接对外提供, 单客户端(官方限制)
type rtspRunner struct {
	oc  *config.OutputConfig
	log *slog.Logger
}

func newRTSPRunner(oc *config.OutputConfig, log *slog.Logger) *rtspRunner {
	return &rtspRunner{oc: oc, log: log}
}

func (r *rtspRunner) Args() []string {
	va, err := BuildVideoArgs(r.oc.Video, "")
	if err != nil {
		r.log.Error("rtsp 视频参数生成失败", "output", r.oc.Name, "err", err)
	}
	aa := BuildAudioArgs(r.oc.Audio)
	args := append(va, aa...)
	if r.oc.RTSPMode == "listen" {
		// ffmpeg 内置 RTSP 服务端
		args = append(args, "-rtsp_flags", "listen", "-rtsp_transport", "tcp")
	} else {
		// 推流到 mediamtx: 使用 TCP 传输更稳定
		args = append(args, "-rtsp_transport", "tcp")
	}
	args = append(args, r.oc.ExtraArgs...)
	args = append(args, "-f", "rtsp", r.oc.Target)
	return args
}

func (r *rtspRunner) Bind(cmd *exec.Cmd) error { return nil }
func (r *rtspRunner) Close() error             { return nil }

func (r *rtspRunner) Status() map[string]any {
	return map[string]any{"mode": r.oc.RTSPMode, "target": r.oc.Target}
}
