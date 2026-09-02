// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
package output

import (
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/FasterEdge/RTSP2Other/internal/config"
)

// hlsRunner 生成 HLS 直播流(fMP4 或 MPEG-TS 切片), 并通过 HTTP 静态服务对外提供。
type hlsRunner struct {
	oc      *config.OutputConfig
	log     *slog.Logger
	dir     string
	pattern string // /streams/<name>-hls/
	prefix  string // 静态文件路由前缀(不含文件名)
}

func newHLSRunner(oc *config.OutputConfig, reg Registrar, log *slog.Logger) (*hlsRunner, error) {
	r := &hlsRunner{
		oc:      oc,
		log:     log,
		dir:     oc.Dir,
		pattern: "/streams/" + oc.Name + "-hls/",
	}
	r.prefix = r.pattern
	if err := os.MkdirAll(r.dir, 0o755); err != nil {
		return nil, err
	}
	fs := http.StripPrefix(r.pattern, http.FileServer(http.Dir(r.dir)))
	if reg != nil {
		reg.Handle(r.pattern, fs)
		// 兼容不带尾斜杠的访问
		reg.Handle("/streams/"+oc.Name+"-hls", http.RedirectHandler(r.pattern, http.StatusMovedPermanently))
	}
	return r, nil
}

func (r *hlsRunner) Args() []string {
	va, err := BuildVideoArgs(r.oc.Video, "")
	if err != nil {
		r.log.Error("hls 视频参数生成失败", "output", r.oc.Name, "err", err)
	}
	aa := BuildAudioArgs(r.oc.Audio)
	args := append(va, aa...)

	args = append(args, "-f", "hls")
	args = append(args, "-hls_time", strconv.Itoa(r.oc.SegmentTime))
	// 注意: hls_playlist_type 只接受 event|vod, "live" 行为 = 不传该选项(默认滚动窗口)
	switch r.oc.PlaylistType {
	case "vod":
		args = append(args, "-hls_playlist_type", "vod", "-hls_list_size", "0")
	case "event":
		args = append(args, "-hls_playlist_type", "event", "-hls_list_size", "0")
	default: // live
		args = append(args, "-hls_list_size", strconv.Itoa(r.oc.SegmentListSize))
	}
	flags := "delete_segments+independent_segments"
	args = append(args, "-hls_flags", flags)
	if r.oc.SegmentType == "fmp4" {
		args = append(args, "-hls_segment_type", "fmp4")
		args = append(args, "-hls_segment_filename", filepath.Join(r.dir, "seg_%04d.m4s"))
	} else {
		args = append(args, "-hls_segment_filename", filepath.Join(r.dir, "seg_%04d.ts"))
	}
	args = append(args, r.oc.ExtraArgs...)
	args = append(args, filepath.Join(r.dir, "index.m3u8"))
	return args
}

func (r *hlsRunner) Bind(cmd *exec.Cmd) error { return nil }
func (r *hlsRunner) Close() error             { return nil }

func (r *hlsRunner) Status() map[string]any {
	return map[string]any{
		"dir":      r.dir,
		"url":      r.pattern + "index.m3u8",
		"playlist": r.oc.PlaylistType,
		"segment":  r.oc.SegmentType,
	}
}
