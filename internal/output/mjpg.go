// FasterEdge 开源项目 - Github: https://github.com/FasterEdge - Gitee: https://gitee.com/FasterEdge
package output

import (
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/FasterEdge/RTSP2Other/internal/config"
	"github.com/FasterEdge/RTSP2Other/internal/mjpg"
)

// mjpgRunner 让 ffmpeg 以 MJPEG 编码输出到管道, Go 侧切分帧并通过 HTTP 广播。
// 多个客户端共享同一个 ffmpeg 进程。
type mjpgRunner struct {
	oc      *config.OutputConfig
	reg     Registrar
	log     *slog.Logger
	hub     *mjpg.Hub
	pattern string

	mu        sync.Mutex
	reader    io.ReadCloser
	readerRun chan struct{} // 读取协程结束信号
}

func newMJpgRunner(oc *config.OutputConfig, reg Registrar, log *slog.Logger) (*mjpgRunner, error) {
	r := &mjpgRunner{
		oc:        oc,
		reg:       reg,
		log:       log,
		hub:       mjpg.NewHub(log),
		pattern:   "/streams/" + oc.Name + ".mjpg",
		readerRun: make(chan struct{}),
	}
	if reg != nil {
		reg.Handle(r.pattern, r.hub)
	}
	return r, nil
}

func (r *mjpgRunner) Args() []string {
	var args []string
	// MJPEG 强制转码(容器不支持 copy 到 mjpeg)
	args = append(args, "-c:v", "mjpeg")
	if r.oc.Quality > 0 {
		args = append(args, "-q:v", strconv.Itoa(r.oc.Quality))
	}
	v := r.oc.Video
	var filters []string
	if v.Scale != "" {
		filters = append(filters, "scale="+v.Scale)
	}
	if v.FPS > 0 {
		filters = append(filters, "fps="+strconv.Itoa(v.FPS))
	}
	if len(filters) > 0 {
		args = append(args, "-vf", strings.Join(filters, ","))
	} else if v.FPS > 0 {
		args = append(args, "-r", strconv.Itoa(v.FPS))
	}
	// MJPEG 无音频轨道
	args = append(args, "-an")
	args = append(args, r.oc.ExtraArgs...)
	args = append(args, "-f", "mjpeg", "pipe:1")
	return args
}

func (r *mjpgRunner) Bind(cmd *exec.Cmd) error {
	pr, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.reader = pr
	rw := make(chan struct{})
	r.readerRun = rw
	r.mu.Unlock()

	go func() {
		defer close(rw) // 关闭本次 Bind 的通道, 避免重启后重复 close 旧通道
		if err := r.hub.Run(pr); err != nil && err != io.EOF {
			r.log.Debug("mjpg 管道读取结束", "output", r.oc.Name, "err", err)
		}
	}()
	return nil
}

func (r *mjpgRunner) Close() error {
	r.mu.Lock()
	if r.reader != nil {
		_ = r.reader.Close()
		r.reader = nil
	}
	rw := r.readerRun
	r.mu.Unlock()
	if rw != nil {
		<-rw
	}
	return nil
}

func (r *mjpgRunner) Status() map[string]any {
	return map[string]any{
		"url":     r.pattern,
		"clients": r.hub.ClientCount(),
	}
}
