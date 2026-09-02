// FasterEdge 开源项目 - Github: https://github.com/FasterEdge - Gitee: https://gitee.com/FasterEdge
package output

import (
	"log/slog"
	"os"
	"os/exec"

	"github.com/FasterEdge/RTSP2Other/internal/config"
)

// stdoutRunner 把转码/复用后的流写到进程 stdout(即工具自身 stdout)。
// 注意: 工具的所有日志都走 stderr, 因此 stdout 是干净的媒体流。
type stdoutRunner struct {
	oc  *config.OutputConfig
	log *slog.Logger
}

func newStdoutRunner(oc *config.OutputConfig, log *slog.Logger) *stdoutRunner {
	return &stdoutRunner{oc: oc, log: log}
}

func (r *stdoutRunner) Args() []string {
	va, err := BuildVideoArgs(r.oc.Video, "")
	if err != nil {
		r.log.Error("stdout 视频参数生成失败", "err", err)
	}
	aa := BuildAudioArgs(r.oc.Audio)
	args := append(va, aa...)
	switch r.oc.Format {
	case "mpegts":
		args = append(args, "-f", "mpegts")
	case "matroska":
		args = append(args, "-f", "matroska")
	case "flv":
		args = append(args, "-f", "flv")
	case "h264":
		args = append(args, "-f", "h264")
	case "hevc":
		args = append(args, "-f", "hevc")
	case "mp4":
		args = append(args, "-movflags", "+frag_keyframe+empty_moov", "-f", "mp4")
	default:
		args = append(args, "-f", "mpegts")
	}
	args = append(args, r.oc.ExtraArgs...)
	args = append(args, "pipe:1")
	return args
}

func (r *stdoutRunner) Bind(cmd *exec.Cmd) error {
	// 媒体流直接继承进程 stdout; 日志全部走 stderr。
	cmd.Stdout = os.Stdout
	return nil
}

func (r *stdoutRunner) Close() error { return nil }

func (r *stdoutRunner) Status() map[string]any {
	return map[string]any{"format": r.oc.Format}
}
