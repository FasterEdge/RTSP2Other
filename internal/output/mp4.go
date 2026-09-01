// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
package output

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"rtsp2other/internal/config"
)

// mp4Runner 写入分段式(fragmented)mp4 文件, 支持 HTTP 范围请求即时播放;
// 若配置了 rotation, 则按时长自动滚动生成多个 mp4 文件。
type mp4Runner struct {
	oc      *config.OutputConfig
	log     *slog.Logger
	pattern string // /streams/<name>.mp4
}

func newMP4Runner(oc *config.OutputConfig, reg Registrar, log *slog.Logger) *mp4Runner {
	r := &mp4Runner{oc: oc, log: log, pattern: "/streams/" + oc.Name + ".mp4"}
	if reg != nil {
		// 用 http.FileServer + http.ServeContent 提供范围请求; 这里直接挂文件 handler
		file := oc.Path
		reg.Handle(r.pattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.ServeFile(w, req, file)
		}))
	}
	return r
}

func (r *mp4Runner) Args() []string {
	va, err := BuildVideoArgs(r.oc.Video, "")
	if err != nil {
		r.log.Error("mp4 视频参数生成失败", "output", r.oc.Name, "err", err)
	}
	aa := BuildAudioArgs(r.oc.Audio)
	// -y: 允许覆盖已存在的输出文件(否则 ffmpeg 在非交互模式下会因
	// "File already exists. Overwrite? [y/N]" 而直接退出)
	args := []string{"-y"}
	args = append(args, va...)
	args = append(args, aa...)

	if r.oc.Rotation != "" {
		// 定时滚动分片: 生成 yyyyMMdd_HHmmss 命名的 mp4
		secs := parseSeconds(r.oc.Rotation, 30)
		if err := os.MkdirAll(filepath.Dir(r.oc.Path), 0o755); err != nil {
			r.log.Warn("创建 mp4 目录失败", "output", r.oc.Name, "err", err)
		}
		base := strings.TrimSuffix(r.oc.Path, filepath.Ext(r.oc.Path))
		pattern := fmt.Sprintf("%s_%%Y%%m%%d_%%H%%M%%S.mp4", base)
		args = append(args,
			"-f", "segment",
			"-segment_time", fmt.Sprintf("%d", secs),
			"-segment_format", "mp4",
			"-segment_atclocktime", "1",
			"-reset_timestamps", "1",
			"-strftime", "1",
			"-movflags", "+frag_keyframe+empty_moov",
		)
		args = append(args, r.oc.ExtraArgs...)
		args = append(args, pattern)
		return args
	}

	// 单文件分段式 mp4
	if err := os.MkdirAll(filepath.Dir(r.oc.Path), 0o755); err != nil {
		r.log.Warn("创建 mp4 目录失败", "output", r.oc.Name, "err", err)
	}
	args = append(args, "-movflags", "+frag_keyframe+empty_moov")
	args = append(args, r.oc.ExtraArgs...)
	args = append(args, "-f", "mp4", r.oc.Path)
	return args
}

func (r *mp4Runner) Bind(cmd *exec.Cmd) error { return nil }
func (r *mp4Runner) Close() error             { return nil }

func (r *mp4Runner) Status() map[string]any {
	return map[string]any{"path": r.oc.Path, "rotation": r.oc.Rotation}
}

func parseSeconds(d string, def int) int {
	if d == "" {
		return def
	}
	if secs, err := time.ParseDuration(d); err == nil {
		if s := int(secs.Seconds()); s > 0 {
			return s
		}
	}
	return def
}
