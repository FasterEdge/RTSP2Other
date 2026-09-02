// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
// Package mtx 管理内置的 mediamtx RTSP 服务端子进程, 为 rtsp(push) 输出提供多客户端 RTSP 服务。
package mtx

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/FasterEdge/RTSP2Other/internal/config"
)

const stateIdle = "idle"
const stateRunning = "running"
const stateFailed = "failed"

// Manager 负责 mediamtx 子进程的启动与守护。
type Manager struct {
	cfg  config.MediaMTXConfig
	path string
	log  *slog.Logger
	conf string // 生成的配置文件路径
	need bool   // 是否需要 mediamtx(存在 push 输出)

	mu        sync.Mutex
	cmd       *exec.Cmd
	state     string
	startedAt time.Time
	lastError string
}

// New 创建 mediamtx 管理器。need=false 时 Start 为空操作。
func New(cfg config.MediaMTXConfig, path string, need bool, log *slog.Logger) *Manager {
	return &Manager{cfg: cfg, path: path, log: log, need: need, state: stateIdle}
}

// Start 生成配置并启动 mediamtx(非阻塞, 内部守护重启)。
func (m *Manager) Start(ctx context.Context) error {
	if !m.need {
		return nil
	}
	conf, err := m.writeConfig()
	if err != nil {
		return err
	}
	m.conf = conf
	go m.loop(ctx)
	return nil
}

// loop 守护 mediamtx 进程。
func (m *Manager) loop(ctx context.Context) {
	delay := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		if delay > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		err := m.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			m.mu.Lock()
			m.lastError = err.Error()
			m.state = stateFailed
			m.mu.Unlock()
			m.log.Error("mediamtx 退出", "err", err, "next_retry", delay)
		}
		delay *= 2
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
	}
}

func (m *Manager) runOnce(ctx context.Context) error {
	args := []string{m.conf}
	if len(m.cfg.ExtraArgs) > 0 {
		args = append(args, m.cfg.ExtraArgs...)
	}
	cmd := exec.CommandContext(ctx, m.path, args...)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	m.mu.Lock()
	m.cmd = cmd
	m.startedAt = time.Now()
	m.state = stateRunning
	m.mu.Unlock()
	m.log.Info("启动 mediamtx", "addr", m.cfg.Listen, "config", m.conf)

	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		m.state = stateFailed
		m.lastError = err.Error()
		m.mu.Unlock()
		return fmt.Errorf("启动 mediamtx 失败: %w", err)
	}
	return cmd.Wait()
}

// writeConfig 生成 mediamtx 配置文件并返回路径。
// 注意: mediamtx v1.20+ 已用 rtspAuthMethods / authInternalUsers 取代旧 authMethods / users。
func (m *Manager) writeConfig() (string, error) {
	type perm struct {
		Action string `yaml:"action"`
	}
	type internalUser struct {
		User        string `yaml:"user"`
		Pass        string `yaml:"pass"`
		Permissions []perm `yaml:"permissions"`
	}
	conf := map[string]any{
		"rtspAddress":    m.cfg.Listen,
		"logLevel":       m.cfg.LogLevel,
		"rtspTransports": []string{"tcp"}, // 仅 TCP: 避免与默认 RTP 端口冲突
		// 只启用 RTSP 服务, 关闭 mediamtx 自带的其它服务, 避免端口冲突与多余开销
		"rtsp":     true,
		"rtmp":     false,
		"hls":      false,
		"webrtc":   false,
		"srt":      false,
		"playback": false,
		"moq":      false,
		"api":      false,
		"metrics":  false,
		"pprof":    false,
		"paths": map[string]any{
			"all": map[string]any{"source": "publisher"},
		},
	}
	if m.cfg.Auth != "" {
		parts := strings.SplitN(m.cfg.Auth, ":", 2)
		if len(parts) != 2 || parts[0] == "" {
			return "", fmt.Errorf("mediamtx.auth 格式应为 user:pass, 得到 %q", m.cfg.Auth)
		}
		conf["rtspAuthMethods"] = []string{"basic"}
		conf["authInternalUsers"] = []internalUser{{
			User: parts[0],
			Pass: parts[1],
			Permissions: []perm{
				{Action: "publish"},
				{Action: "read"},
				{Action: "playback"},
			},
		}}
	}
	data, err := yaml.Marshal(conf)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(".", ".mediamtx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "mediamtx.yml")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// SignalStop 给 mediamtx 发送信号(优雅退出)。
func (m *Manager) SignalStop(sig syscall.Signal) {
	m.mu.Lock()
	cmd := m.cmd
	m.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Signal(sig)
	}
}

// Status 返回状态摘要。
func (m *Manager) Status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := map[string]any{"state": m.state, "addr": m.cfg.Listen}
	if !m.startedAt.IsZero() {
		s["uptime_seconds"] = int64(time.Since(m.startedAt).Seconds())
	}
	if m.lastError != "" {
		s["last_error"] = m.lastError
	}
	return s
}
