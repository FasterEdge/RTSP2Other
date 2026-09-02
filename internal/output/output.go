package output

import (
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"

	"github.com/FasterEdge/RTSP2Other/internal/config"
)

// Registrar 允许 Runner 在启动前注册 HTTP 路由(由 HTTP 服务实现)。
type Registrar interface {
	Handle(pattern string, h http.Handler)
}

// Runner 抽象一个输出管道在 ffmpeg 进程之外需要管理的资源。
type Runner interface {
	// Args 返回追加在输入参数之后的 ffmpeg 输出参数。
	Args() []string
	// Bind 在 cmd.Start() 之前被调用, 可用来接管 stdout 管道或注册内容处理器。
	Bind(cmd *exec.Cmd) error
	// Close 在引擎关闭或输出彻底停止时被调用, 释放资源。
	Close() error
	// Status 返回供 /status.json 展示的额外信息。
	Status() map[string]any
}

// Build 根据输出配置构造对应的 Runner。
func Build(oc *config.OutputConfig, reg Registrar, log *slog.Logger) (Runner, error) {
	switch oc.Type {
	case "stdout":
		return newStdoutRunner(oc, log), nil
	case "http_mjpg":
		return newMJpgRunner(oc, reg, log)
	case "rtsp":
		return newRTSPRunner(oc, log), nil
	case "mp4":
		return newMP4Runner(oc, reg, log), nil
	case "hls":
		r, err := newHLSRunner(oc, reg, log)
		if err != nil {
			return nil, fmt.Errorf("output %q: %w", oc.Name, err)
		}
		return r, nil
	default:
		return nil, fmt.Errorf("未知输出类型 %q", oc.Type)
	}
}
