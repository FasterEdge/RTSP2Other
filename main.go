// FasterEdge 开源项目 · https://github.com/FasterEdge · https://gitee.com/FasterEdge
// rtsp2other: 用 Go 管理 ffmpeg 子进程, 把一路 RTSP 输入转换为
// stdout / HTTP-MJPG / 本地 RTSP / mp4 流式文件 / HLS 等任意多路输出。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rtsp2other/internal/config"
	"rtsp2other/internal/engine"
	"rtsp2other/internal/httpserv"
	"rtsp2other/internal/mtx"
	"rtsp2other/internal/output"
)

var version = "1.0.20260831" // 通过 -ldflags "-X main.version=..." 覆盖

func main() {
	cfgPath := flag.String("config", "rtsp2other.yaml", "配置文件路径")
	once := flag.Bool("once", false, "任一输出进程退出后停止全部(适合脚本/一次性任务)")
	check := flag.Bool("check", false, "只校验配置并打印摘要, 不启动服务")
	showVer := flag.Bool("version", false, "打印版本号并退出")
	logLevel := flag.String("log", "info", "日志级别: debug|info|warn|error")
	flag.Parse()

	if *showVer {
		fmt.Printf("rtsp2other %s\n", version)
		os.Exit(0)
	}
	logger := newLogger(*logLevel)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		logger.Error("配置加载失败", "err", err)
		os.Exit(1)
	}
	printSummary(cfg, logger)
	if *check {
		return
	}
	if !cfg.HTTP.Enabled {
		for i := range cfg.Outputs {
			o := &cfg.Outputs[i]
			if *o.Enabled && o.Type == "http_mjpg" {
				logger.Error("http_mjpg 输出需要启用 http 服务(http.enabled: true)")
				os.Exit(1)
			}
		}
	}

	eng := engine.New(cfg, logger, *once)

	var mtxMgr *mtx.Manager // 在创建 HTTP 服务前声明, 供状态闭包引用
	var srv *httpserv.Server
	if cfg.HTTP.Enabled {
		mtxFn := func() map[string]any {
			if mtxMgr != nil {
				return mtxMgr.Status()
			}
			return nil
		}
		srv = httpserv.New(cfg.HTTP, cfg.Outputs, eng.Snapshot, mtxFn, logger)
	}
	// 占位 registrar: HTTP 关闭时, 文件类输出仍可工作
	noop := noopRegistrar{}
	registrar := func() output.Registrar {
		if srv != nil {
			return srv
		}
		return noop
	}

	for i := range cfg.Outputs {
		oc := &cfg.Outputs[i]
		if !*oc.Enabled {
			continue
		}
		runner, err := output.Build(oc, registrar(), logger)
		if err != nil {
			logger.Error("构造输出失败", "name", oc.Name, "err", err)
			os.Exit(1)
		}
		eng.Add(oc, runner)
		logger.Info("已注册输出", "name", oc.Name, "type", oc.Type)
	}

	// 信号上下文: Ctrl+C / SIGTERM 优雅退出
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// mediamtx 守护(仅当存在 rtsp push 输出)
	if cfg.HasRTSPPush() {
		mtxMgr = mtx.New(cfg.MediaMTX, cfg.MTXPath, true, logger.With("component", "mediamtx"))
		mtxCtx, mtxCancel := context.WithCancel(context.Background())
		defer mtxCancel()
		if err := mtxMgr.Start(mtxCtx); err != nil {
			logger.Error("启动 mediamtx 失败", "err", err)
			os.Exit(1)
		}
	}
	if srv != nil {
		if err := srv.Start(); err != nil {
			logger.Error("启动 HTTP 服务失败", "err", err)
			os.Exit(1)
		}
	}

	eng.Start()
	logger.Info("rtsp2other 已启动", "config", *cfgPath, "once", *once)

	<-ctx.Done()
	logger.Info("收到退出信号, 正在停止...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eng.Shutdown()
	if mtxMgr != nil {
		mtxMgr.SignalStop(syscall.SIGTERM)
		time.Sleep(500 * time.Millisecond)
		mtxMgr.SignalStop(syscall.SIGKILL)
	}
	if srv != nil {
		_ = srv.Shutdown(shutdownCtx)
	}
	logger.Info("已退出")
}

// noopRegistrar 在 HTTP 关闭时吞掉路由注册。
type noopRegistrar struct{}

func (noopRegistrar) Handle(string, http.Handler) {}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn", "warning":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv})
	return slog.New(h)
}

func printSummary(cfg *config.Config, logger *slog.Logger) {
	logger.Info("配置摘要",
		"input", cfg.Input.URL,
		"transport", cfg.Input.Transport,
		"outputs", len(cfg.Outputs),
	)
	for i := range cfg.Outputs {
		o := &cfg.Outputs[i]
		logger.Info("  - 输出",
			"name", o.Name,
			"type", o.Type,
			"enabled", *o.Enabled,
			"restart", o.Restart != nil && *o.Restart,
		)
	}
	if cfg.FFmpegPath != "" {
		logger.Info("ffmpeg", "path", cfg.FFmpegPath)
	}
	if cfg.MTXPath != "" {
		logger.Info("mediamtx", "path", cfg.MTXPath)
	}
}
