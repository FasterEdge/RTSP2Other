// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
package output

import (
	"log/slog"
	"net/url"
	"os/exec"

	"github.com/FasterEdge/RTSP2Other/internal/config"
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

// redactURL 掩码 URL 中的 userinfo(用户名:密码), 避免 /status.json 匿名泄露
// 推流凭据(如 rtsp://user:pass@host 明文暴露给未授权客户端)。
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User("***")
	return u.String()
}

func (r *rtspRunner) Status() map[string]any {
	return map[string]any{"mode": r.oc.RTSPMode, "target": redactURL(r.oc.Target)}
}
