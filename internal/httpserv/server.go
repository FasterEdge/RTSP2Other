// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
// Package httpserv 提供内置 HTTP 服务: 首页、健康检查、状态 JSON、各流类型的播放页。
// 所有流媒体输出(MJPG/HLS/MP4)共享这一个监听端口, 通过路径区分。
package httpserv

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/FasterEdge/RTSP2Other/internal/config"
	"github.com/FasterEdge/RTSP2Other/internal/engine"
)

// StatusProvider 由引擎实现, 提供各输出的实时状态。
type StatusProvider func() []engine.Status

// Server 内置 HTTP 服务。
type Server struct {
	cfg     config.HTTPConfig
	log     *slog.Logger
	mtxFn   func() map[string]any
	status  StatusProvider
	outputs []config.OutputConfig
	mux     *http.ServeMux
	srv     *http.Server
	prefix  string
}

// New 创建 HTTP 服务。mtxFn 可为 nil。
func New(cfg config.HTTPConfig, outputs []config.OutputConfig, status StatusProvider, mtxFn func() map[string]any, log *slog.Logger) *Server {
	s := &Server{
		cfg:     cfg,
		log:     log,
		mtxFn:   mtxFn,
		status:  status,
		outputs: outputs,
		mux:     http.NewServeMux(),
		prefix:  cfg.Prefix,
	}
	p := s.prefix
	s.mux.Handle(p+"/healthz", http.HandlerFunc(s.handleHealth))
	s.mux.Handle(p+"/status.json", http.HandlerFunc(s.handleStatus))
	s.mux.Handle(p+"/", http.HandlerFunc(s.handleLanding))
	s.mux.Handle(p+"/play/", http.HandlerFunc(s.handlePlay))
	return s
}

// Handle 注册路由(自动加前缀)。由各输出 Runner 在构建时调用。
func (s *Server) Handle(pattern string, h http.Handler) {
	s.mux.Handle(s.prefix+pattern, h)
}

// Start 启动 HTTP 监听。
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("HTTP 监听 %s 失败: %w", s.cfg.Listen, err)
	}
	s.srv = &http.Server{Handler: s.mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.log.Error("HTTP 服务异常退出", "err", err)
		}
	}()
	s.log.Info("HTTP 服务已启动", "addr", s.cfg.Listen, "prefix", s.prefix)
	return nil
}

// Addr 返回实际监听地址(便于日志展示)。
func (s *Server) Addr() string {
	if s.srv == nil {
		return s.cfg.Listen
	}
	return s.cfg.Listen
}

// Shutdown 优雅关闭。
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{"outputs": []engine.Status{}}
	if s.status != nil {
		payload["outputs"] = s.status()
	} else {
		payload["outputs"] = []engine.Status{}
	}
	if s.mtxFn != nil {
		payload["mediamtx"] = s.mtxFn()
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(payload)
}

// handleLanding 输出首页: 列出所有输出与播放入口。
func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != s.prefix+"/" && r.URL.Path != s.prefix {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html lang='zh'><head><meta charset='utf-8'>")
	b.WriteString("<title>RTSP2Other</title>")
	b.WriteString("<style>body{font-family:system-ui;margin:2rem;background:#111;color:#eee}li{margin:.5rem 0}a{color:#7cc}code{background:#222;padding:2px 6px;border-radius:4px}</style>")
	b.WriteString("</head><body><h1>RTSP2Other</h1><p>输入: <code>" + html.EscapeString(s.inputURL()) + "</code></p>")
	b.WriteString("<h2>输出管道</h2><ul>")
	for i := range s.outputs {
		o := &s.outputs[i]
		if o.Enabled != nil && !*o.Enabled {
			continue
		}
		b.WriteString("<li><b>" + html.EscapeString(o.Name) + "</b> (" + html.EscapeString(o.Type) + ") — ")
		b.WriteString(s.outputLinks(o))
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	b.WriteString("<p><a href='" + s.prefix + "/status.json'>status.json</a> | <a href='" + s.prefix + "/healthz'>healthz</a></p>")
	b.WriteString("</body></html>")
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) inputURL() string {
	// 仅用于首页展示; 具体输入由引擎日志给出
	return "见配置文件 input.url"
}

func (s *Server) outputLinks(o *config.OutputConfig) string {
	p := s.prefix
	switch o.Type {
	case "http_mjpg":
		return fmt.Sprintf(`<a href="%s/streams/%s.mjpg">/streams/%s.mjpg</a> | <a href="%s/play/%s.mjpg">播放页</a>`,
			p, o.Name, o.Name, p, o.Name)
	case "rtsp":
		tgt := o.Target
		if tgt == "" {
			tgt = "(未配置 target)"
		}
		return fmt.Sprintf(`<code>%s</code> (可用 VLC / ffplay 播放)`, html.EscapeString(tgt))
	case "mp4":
		return fmt.Sprintf(`<a href="%s/streams/%s.mp4">/streams/%s.mp4</a> | <a href="%s/play/%s.mp4">播放页</a> | 文件: <code>%s</code>`,
			p, o.Name, o.Name, p, o.Name, html.EscapeString(o.Path))
	case "hls":
		return fmt.Sprintf(`<a href="%s/streams/%s-hls/index.m3u8">index.m3u8</a> | <a href="%s/play/%s.m3u8">播放页</a>`,
			p, o.Name, p, o.Name)
	case "stdout":
		return fmt.Sprintf("写入进程 stdout, 格式 <code>%s</code>", html.EscapeString(o.Format))
	default:
		return ""
	}
}

// handlePlay 提供内嵌播放页。
func (s *Server) handlePlay(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, s.prefix+"/play/")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if strings.HasSuffix(name, ".mjpg") {
		n := strings.TrimSuffix(name, ".mjpg")
		fmt.Fprintf(w, pageMJPG, s.prefix, n)
		return
	}
	if strings.HasSuffix(name, ".mp4") {
		n := strings.TrimSuffix(name, ".mp4")
		fmt.Fprintf(w, pageMP4, s.prefix, n)
		return
	}
	if strings.HasSuffix(name, ".m3u8") {
		n := strings.TrimSuffix(name, ".m3u8")
		fmt.Fprintf(w, pageHLS, s.prefix, n)
		return
	}
	http.NotFound(w, r)
}

const pageMJPG = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>MJPG</title></head>
<body style="background:#111;margin:0"><img src="%s/streams/%s.mjpg" style="width:100%%;max-width:1200px;display:block;margin:auto"></body></html>`

const pageMP4 = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>MP4 流式</title></head>
<body style="background:#111;margin:0">
<video controls autoplay muted playsinline src="%s/streams/%s.mp4" style="width:100%%;max-width:1200px;display:block;margin:auto"></video>
</body></html>`

const pageHLS = `<!DOCTYPE html><html><head><meta charset="utf-8"><title>HLS</title>
<script src="https://cdn.jsdelivr.net/npm/hls.js@1"></script></head>
<body style="background:#111;margin:0">
<video id="v" controls autoplay muted playsinline style="width:100%%;max-width:1200px;display:block;margin:auto"></video>
<script>
(function(){
  var url = "%s/streams/%s-hls/index.m3u8";
  var v = document.getElementById("v");
  if (window.Hls && Hls.isSupported()) { var h = new Hls(); h.loadSource(url); h.attachMedia(v); }
  else if (v.canPlayType("application/vnd.apple.mpegurl")) { v.src = url; }
  else { document.body.innerHTML = "<p style='color:#fff'>浏览器不支持 HLS, 请使用 Chrome/Edge 或 Safari</p>"; }
})();
</script></body></html>`
