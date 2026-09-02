// Package engine 负责 ffmpeg 子进程的完整生命周期管理:
// 启动、监控、崩溃重启(指数退避)、优雅关闭, 以及输入探测。
package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/FasterEdge/RTSP2Other/internal/config"
	"github.com/FasterEdge/RTSP2Other/internal/output"
)

const (
	stateIdle       = "idle"
	stateStarting   = "starting"
	stateRunning    = "running"
	stateRestarting = "restarting"
	stateFailed     = "failed"
	stateStopped    = "stopped"

	shutdownGrace   = 6 * time.Second  // SIGTERM 后等待子进程退出的宽限时间
	stableThreshold = 30 * time.Second // 稳定运行超过该时长后, 重启退避重置为初始值
)

// Status 是运行状态快照(供 /status.json 与日志使用)。
type Status struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	State     string         `json:"state"`
	PID       int            `json:"pid,omitempty"`
	Restarts  int            `json:"restarts"`
	UptimeSec int64          `json:"uptime_seconds,omitempty"`
	LastExit  string         `json:"last_exit,omitempty"`
	LastError string         `json:"last_error,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// Engine 管理所有输出管道。
type Engine struct {
	cfg             *config.Config
	log             *slog.Logger
	once            bool
	watchdogTimeout time.Duration

	mtx     sync.Mutex
	outputs []*Output
	cancel  context.CancelFunc
	ctx     context.Context

	stopOnce sync.Once
}

// New 创建引擎。
func New(cfg *config.Config, log *slog.Logger, once bool) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	wd := mustParseDur(cfg.FFmpeg.WatchdogTimeout, 30*time.Second)
	return &Engine{cfg: cfg, log: log, once: once, ctx: ctx, cancel: cancel, watchdogTimeout: wd}
}

// Add 注册一个输出管道(需要在 Start 前完成)。
func (e *Engine) Add(oc *config.OutputConfig, runner output.Runner) *Output {
	o := &Output{
		engine:    e,
		cfg:       oc,
		runner:    runner,
		log:       e.log.With("output", oc.Name),
		done:      make(chan struct{}),
		state:     stateIdle,
		initDelay: mustParseDur(oc.RestartDelay, time.Second),
		maxDelay:  mustParseDur(oc.MaxRestartDelay, 30*time.Second),
	}
	e.mtx.Lock()
	e.outputs = append(e.outputs, o)
	e.mtx.Unlock()
	return o
}

// Start 启动所有输出管道(非阻塞)。
func (e *Engine) Start() {
	if runtime.GOOS == "windows" {
		e.log.Warn("Windows 平台不支持无进展看门狗(ExtraFiles 限制), 已自动禁用; 崩溃重启/输入重连仍生效")
	}
	inputs := map[string][]string{}
	for _, o := range e.outputs {
		ic := &e.cfg.Input
		if o.cfg.Input != nil {
			ic = o.cfg.Input
		}
		inputArgs, ok := inputs[ic.URL]
		if !ok {
			inputArgs = output.BuildInputArgs(ic)
			inputs[ic.URL] = inputArgs
		}
		go o.loop(e.ctx, inputArgs)
	}
	// 输入探测(可选)
	if e.cfg.FFmpeg.Probe != nil && *e.cfg.FFmpeg.Probe {
		for _, o := range e.outputs {
			ic := &e.cfg.Input
			if o.cfg.Input != nil {
				ic = o.cfg.Input
			}
			e.probe(ic.URL)
			break // 全局输入探测一次即可(自定义输入由各输出日志观察)
		}
	}
}

// probe 用 ffprobe 探测输入并打印摘要。
func (e *Engine) probe(url string) {
	ffprobe := e.ffprobePath()
	if ffprobe == "" {
		e.log.Warn("未找到 ffprobe, 跳过输入探测")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error", "-print_format", "json", "-show_streams", "-show_format", url)
	out, err := cmd.Output()
	if err != nil {
		e.log.Debug("ffprobe 探测失败", "url", url, "err", err)
		return
	}
	var info struct {
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
			BitRate  string `json:"bit_rate"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return
	}
	var parts []string
	for _, s := range info.Streams {
		switch s.CodecType {
		case "video":
			fps := strings.TrimSuffix(s.RFrameRate, "/1")
			if strings.Contains(s.RFrameRate, "/") {
				var a, b int
				if _, err := fmt.Sscanf(s.RFrameRate, "%d/%d", &a, &b); err == nil && b > 0 {
					fps = fmt.Sprintf("%.2f", float64(a)/float64(b))
				}
			}
			parts = append(parts, fmt.Sprintf("video=%s %dx%d %sfps", s.CodecName, s.Width, s.Height, fps))
		case "audio":
			parts = append(parts, fmt.Sprintf("audio=%s", s.CodecName))
		}
	}
	if info.Format.Duration != "" {
		parts = append(parts, "duration="+info.Format.Duration+"s")
	}
	e.log.Info("输入探测", "url", url, "streams", strings.Join(parts, ", "))
}

func (e *Engine) ffprobePath() string {
	if e.cfg.FFmpegPath != "" {
		p := filepath.Join(filepath.Dir(e.cfg.FFmpegPath), "ffprobe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return p
	}
	return ""
}

// Shutdown 优雅关闭所有子进程并等待退出。
func (e *Engine) Shutdown() {
	e.cancel() // 通知所有循环停止
	for _, o := range e.outputs {
		o.signalStop(syscall.SIGTERM)
	}
	done := make(chan struct{})
	go func() {
		for _, o := range e.outputs {
			<-o.done
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		e.log.Warn("部分 ffmpeg 进程未在宽限期内退出, 发送 SIGKILL")
		for _, o := range e.outputs {
			o.signalStop(syscall.SIGKILL)
		}
		<-done
	}
	// 释放各 runner 资源
	for _, o := range e.outputs {
		_ = o.runner.Close()
	}
}

// requestStop 在 once 模式下由第一个退出的输出触发, 停止全部。
func (e *Engine) requestStop() {
	e.stopOnce.Do(func() {
		e.log.Info("once 模式: 检测到输出退出, 停止全部管道")
		e.cancel()
	})
}

// Snapshot 返回所有输出的状态快照。
func (e *Engine) Snapshot() []Status {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	out := make([]Status, 0, len(e.outputs))
	for _, o := range e.outputs {
		out = append(out, o.status())
	}
	return out
}

// Output 是单个输出管道。
type Output struct {
	engine *Engine
	cfg    *config.OutputConfig
	runner output.Runner
	log    *slog.Logger
	done   chan struct{}

	initDelay time.Duration
	maxDelay  time.Duration

	mu        sync.Mutex
	cmd       *exec.Cmd
	state     string
	pid       int
	restarts  int
	startedAt time.Time
	lastExit  string
	lastError string
}

func (o *Output) finish() { close(o.done) }

func (o *Output) setState(s string) {
	o.mu.Lock()
	o.state = s
	o.mu.Unlock()
}

// signalStop 给当前子进程发送信号(进程可能已退出, 忽略错误)。
func (o *Output) signalStop(sig syscall.Signal) {
	o.mu.Lock()
	cmd := o.cmd
	o.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(sig)
	}
}

// loop 是每个输出的主循环: 启动 → 等待退出 → 按退避策略重启。
func (o *Output) loop(ctx context.Context, inputArgs []string) {
	defer o.finish()
	delay := o.initDelay
	for {
		if ctx.Err() != nil {
			o.setState(stateStopped)
			return
		}
		if delay > 0 {
			o.setState(stateRestarting)
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				o.setState(stateStopped)
				return
			case <-t.C:
			}
		}
		err := o.runOnce(ctx, inputArgs)
		if ctx.Err() != nil {
			o.setState(stateStopped)
			return
		}
		if o.engine.once {
			o.engine.requestStop()
			return
		}
		if o.cfg.Restart == nil || !*o.cfg.Restart {
			o.setState(stateFailed)
			o.log.Warn("输出已停止(restart=false)", "err", err)
			return
		}
		// 指数退避: 稳定运行过阈值则重置
		uptime := o.uptime()
		if uptime >= stableThreshold {
			delay = o.initDelay
		} else {
			delay *= 2
			if delay > o.maxDelay {
				delay = o.maxDelay
			}
		}
		o.mu.Lock()
		o.restarts++
		if err != nil {
			o.lastError = err.Error()
		}
		o.mu.Unlock()
		o.log.Warn("输出退出, 准备重启", "err", err, "next_retry", delay.Round(time.Millisecond))
	}
}

// runOnce 执行一次 ffmpeg 进程并等待其退出, 返回退出原因。
func (o *Output) runOnce(ctx context.Context, inputArgs []string) error {
	args := o.buildArgs(inputArgs)

	// 无进展守护: 通过 ffmpeg 的 -progress 输出判断进程是否仍在出帧。
	// 注意: Windows 不支持 ExtraFiles(fd 3+), 会直接报错, 因此在 Windows 上
	// 禁用看门狗, 仅靠进程守护重启 + 输入重连兜底(见 Engine.Start 的日志说明)。
	var progressR, pw *os.File
	var watchdogStart func()
	if o.engine.watchdogTimeout > 0 && runtime.GOOS != "windows" {
		pr, pww, err := os.Pipe()
		if err == nil {
			progressR = pr
			pw = pww
			args = append([]string{"-progress", "pipe:3"}, args...)
			watchdogStart = o.startWatchdog(ctx, progressR, o.engine.watchdogTimeout)
		} else {
			o.log.Warn("创建 progress 管道失败, 无进展守护未启用", "err", err)
		}
	}

	o.log.Info("启动 ffmpeg", "cmd", strings.Join(args, " "))

	cmd := exec.Command(o.engine.cfg.FFmpegPath, args...)
	cmd.Env = os.Environ()
	cmd.Stderr = &lineWriter{log: o.log, prefix: "[ffmpeg] "}
	if progressR != nil {
		// fd 3 交给子进程写入 progress, 父进程持有读端。
		cmd.ExtraFiles = []*os.File{pw}
	}
	if err := o.runner.Bind(cmd); err != nil {
		o.log.Error("绑定输出资源失败", "err", err)
		return err
	}

	o.mu.Lock()
	o.cmd = cmd
	o.startedAt = time.Now()
	o.state = stateStarting
	o.mu.Unlock()

	if err := cmd.Start(); err != nil {
		o.mu.Lock()
		o.state = stateFailed
		o.lastError = err.Error()
		o.mu.Unlock()
		return fmt.Errorf("启动 ffmpeg 失败: %w", err)
	}
	if progressR != nil {
		_ = pw.Close() // 子进程已持有写端副本
		watchdogStart()
	}

	o.mu.Lock()
	o.pid = cmd.Process.Pid
	o.state = stateRunning
	o.mu.Unlock()
	o.log.Info("ffmpeg 运行中", "pid", o.pid)

	err := cmd.Wait()
	if progressR != nil {
		_ = progressR.Close() // 让 progress 读取协程结束
	}
	exitInfo := "exit 0"
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitInfo = fmt.Sprintf("exit code %d", ee.ExitCode())
		} else {
			exitInfo = err.Error()
		}
	}
	o.mu.Lock()
	o.lastExit = exitInfo
	o.state = stateIdle
	o.pid = 0
	o.mu.Unlock()
	o.log.Info("ffmpeg 退出", "reason", exitInfo)
	return err
}

// startWatchdog 启动无进展守护, 返回立即启动 watchdog 循环的函数。
// 原理: 读取 ffmpeg -progress(写入 fd 3)中的 out_time_ms, 若超过 timeout
// 没有新进展(或首次出帧前卡住), 则重启进程。对任意 ffmpeg 构建均可用。
func (o *Output) startWatchdog(ctx context.Context, progressR *os.File, timeout time.Duration) func() {
	var (
		mu           sync.Mutex
		lastAct      = time.Now()
		progressDone = make(chan struct{})
	)

	go func() { // 读 progress 流
		defer close(progressDone)
		sc := bufio.NewScanner(progressR)
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "out_time_ms=") {
				if v, err := strconv.ParseInt(strings.TrimPrefix(line, "out_time_ms="), 10, 64); err == nil && v > 0 {
					mu.Lock()
					lastAct = time.Now()
					mu.Unlock()
				}
			}
			if line == "progress=end" {
				return
			}
		}
	}()

	return func() {
		go func() {
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-progressDone:
					return
				case <-t.C:
					mu.Lock()
					since := time.Since(lastAct)
					mu.Unlock()
					if since > timeout {
						o.log.Warn("输出无进展超时, 触发重启",
							"stalled_for", since.Round(time.Second), "timeout", timeout)
						o.signalStop(syscall.SIGTERM)
						select {
						case <-progressDone:
							return
						case <-time.After(5 * time.Second):
							o.log.Warn("进程未响应 SIGTERM, 发送 SIGKILL")
							o.signalStop(syscall.SIGKILL)
							return
						}
					}
				}
			}
		}()
	}
}

// buildArgs 组装完整 ffmpeg 命令参数。
func (o *Output) buildArgs(inputArgs []string) []string {
	var args []string
	// 全局参数
	if len(o.engine.cfg.FFmpeg.GlobalArgs) > 0 {
		args = append(args, o.engine.cfg.FFmpeg.GlobalArgs...)
	} else {
		args = append(args, "-hide_banner")
	}
	args = append(args, "-loglevel", o.engine.cfg.FFmpeg.LogLevel)
	args = append(args, inputArgs...)
	args = append(args, o.runner.Args()...)
	return args
}

func (o *Output) uptime() time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.startedAt.IsZero() {
		return 0
	}
	return time.Since(o.startedAt)
}

func (o *Output) status() Status {
	o.mu.Lock()
	defer o.mu.Unlock()
	s := Status{
		Name:      o.cfg.Name,
		Type:      o.cfg.Type,
		State:     o.state,
		PID:       o.pid,
		Restarts:  o.restarts,
		LastExit:  o.lastExit,
		LastError: o.lastError,
	}
	if !o.startedAt.IsZero() {
		s.UptimeSec = int64(time.Since(o.startedAt).Seconds())
	}
	if o.runner != nil {
		s.Extra = o.runner.Status()
	}
	return s
}

// lineWriter 把子进程 stderr 按行写入日志。
type lineWriter struct {
	log    *slog.Logger
	prefix string
	buf    []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if line != "" {
			w.log.Info(w.prefix + line)
		}
	}
	if len(w.buf) > 4096 { // 防呆: 无换行的超长输出直接丢
		w.buf = w.buf[len(w.buf)-1024:]
	}
	return len(p), nil
}

// mustParseDur 解析时长字符串, 失败时返回默认值。
func mustParseDur(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return d
	}
	return def
}
