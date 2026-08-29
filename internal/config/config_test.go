package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTmp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cfg.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaultsAndValidation(t *testing.T) {
	p := writeTmp(t, `
input:
  url: "rtsp://x:8554/s"
outputs:
  - name: a
    type: stdout
`)
	cfg, err := LoadForCheck(p)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.Input.Transport != "tcp" {
		t.Errorf("transport 默认应为 tcp, 得到 %s", cfg.Input.Transport)
	}
	if cfg.HTTP.Listen != "0.0.0.0:8080" {
		t.Errorf("http.listen 默认应为 0.0.0.0:8080")
	}
	o := cfg.Outputs[0]
	if o.Enabled == nil || !*o.Enabled {
		t.Error("enabled 默认应为 true")
	}
	if o.Format != "mpegts" {
		t.Errorf("stdout format 默认应为 mpegts")
	}
	if o.Video.Mode != "copy" {
		t.Errorf("stdout video.mode 默认应为 copy")
	}
}

func TestMissingURLFails(t *testing.T) {
	p := writeTmp(t, `
http:
  enabled: false
outputs:
  - name: a
    type: stdout
`)
	if _, err := LoadForCheck(p); err == nil {
		t.Fatal("缺少 input.url 应报错")
	}
}

func TestDuplicateOutputNameFails(t *testing.T) {
	p := writeTmp(t, `
input:
  url: "rtsp://x/s"
outputs:
  - name: a
    type: stdout
  - name: a
    type: http_mjpg
`)
	if _, err := LoadForCheck(p); err == nil {
		t.Fatal("重名输出应报错")
	}
}

func TestTwoStdoutFails(t *testing.T) {
	p := writeTmp(t, `
input:
  url: "rtsp://x/s"
outputs:
  - name: a
    type: stdout
  - name: b
    type: stdout
`)
	if _, err := LoadForCheck(p); err == nil {
		t.Fatal("多个 stdout 输出应报错")
	}
}

func TestUnknownTypeFails(t *testing.T) {
	p := writeTmp(t, `
input:
  url: "rtsp://x/s"
outputs:
  - name: a
    type: bogus
`)
	if _, err := LoadForCheck(p); err == nil {
		t.Fatal("未知输出类型应报错")
	}
}

func TestMJPGDefaults(t *testing.T) {
	p := writeTmp(t, `
input:
  url: "rtsp://x/s"
outputs:
  - name: a
    type: http_mjpg
`)
	cfg, err := LoadForCheck(p)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	o := cfg.Outputs[0]
	if o.Video.Codec != "mjpeg" || o.Video.FPS != 15 || o.Quality != 3 {
		t.Errorf("mjpg 默认值错误: codec=%s fps=%d quality=%d", o.Video.Codec, o.Video.FPS, o.Quality)
	}
	if o.Audio.Mode != "disable" {
		t.Error("mjpg audio 默认应为 disable")
	}
}

func TestRTSPPushDefaultTarget(t *testing.T) {
	p := writeTmp(t, `
input:
  url: "rtsp://x/s"
outputs:
  - name: cam
    type: rtsp
`)
	cfg, err := LoadForCheck(p)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	o := cfg.Outputs[0]
	if o.RTSPMode != "push" {
		t.Error("rtsp 默认模式应为 push")
	}
	if o.Target != "rtsp://127.0.0.1:8554/cam" {
		t.Errorf("push 默认 target 错误: %s", o.Target)
	}
}

func TestHLSDefaults(t *testing.T) {
	p := writeTmp(t, `
input:
  url: "rtsp://x/s"
outputs:
  - name: h
    type: hls
`)
	cfg, err := LoadForCheck(p)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	o := cfg.Outputs[0]
	if o.SegmentTime != 4 || o.SegmentListSize != 6 || o.SegmentType != "fmp4" {
		t.Errorf("hls 默认值错误: %+v", o)
	}
}
