package output

import (
	"io"
	"log/slog"
	"reflect"
	"testing"

	"rtsp2other/internal/config"
)

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildInputArgsRTSPTCP(t *testing.T) {
	args := BuildInputArgs(&config.InputConfig{
		URL:             "rtsp://u:p@host:554/stream",
		Transport:       "tcp",
		SocketTimeoutMS: 5000,
	})
	want := []string{"-rtsp_transport", "tcp", "-rw_timeout", "5000000", "-i", "rtsp://u:p@host:554/stream"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got  %v\nwant %v", args, want)
	}
}

func TestBuildInputArgsUDPNoTimeout(t *testing.T) {
	args := BuildInputArgs(&config.InputConfig{
		URL:             "rtsp://host/stream",
		Transport:       "udp",
		SocketTimeoutMS: 5000,
	})
	if args[0] != "-rtsp_transport" || args[1] != "udp" {
		t.Errorf("udp 传输参数错误: %v", args)
	}
	for _, a := range args {
		if a == "-rw_timeout" {
			t.Error("udp 传输不应注入 -rw_timeout")
		}
	}
}

func TestBuildVideoArgsCopy(t *testing.T) {
	args, err := BuildVideoArgs(config.VideoConfig{Mode: "copy"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"-c:v", "copy"}) {
		t.Errorf("copy 模式参数错误: %v", args)
	}
}

func TestBuildVideoArgsTranscode(t *testing.T) {
	args, err := BuildVideoArgs(config.VideoConfig{
		Mode:    "transcode",
		Codec:   "h264",
		Bitrate: "1M",
		Preset:  "veryfast",
		FPS:     25,
		Scale:   "640x360",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-c:v", "libx264", "-preset", "veryfast", "-b:v", "1M", "-vf", "scale=640x360,fps=25"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got  %v\nwant %v", args, want)
	}
}

func TestBuildVideoArgsHardware(t *testing.T) {
	args, err := BuildVideoArgs(config.VideoConfig{Mode: "transcode", Codec: "h264"}, "h264_nvenc")
	if err != nil {
		t.Fatal(err)
	}
	if args[1] != "h264_nvenc" {
		t.Errorf("硬件编码器未生效: %v", args)
	}
}

func TestBuildAudioArgsDisable(t *testing.T) {
	args := BuildAudioArgs(config.AudioConfig{Mode: "disable"})
	if !reflect.DeepEqual(args, []string{"-an"}) {
		t.Errorf("disable 参数错误: %v", args)
	}
}

func TestBuildAudioArgsTranscode(t *testing.T) {
	args := BuildAudioArgs(config.AudioConfig{
		Mode:       "transcode",
		Codec:      "aac",
		Bitrate:    "128k",
		SampleRate: 44100,
		Channels:   2,
	})
	want := []string{"-c:a", "aac", "-b:a", "128k", "-ar", "44100", "-ac", "2"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("got  %v\nwant %v", args, want)
	}
}

func TestMJPGArgs(t *testing.T) {
	r, err := newMJpgRunner(&config.OutputConfig{
		Name:    "c",
		Type:    "http_mjpg",
		Quality: 3,
		Video:   config.VideoConfig{FPS: 15, Scale: "320x180"},
	}, nil, testLog())
	if err != nil {
		t.Fatal(err)
	}
	args := r.Args()
	if !reflect.DeepEqual(args[:2], []string{"-c:v", "mjpeg"}) {
		t.Errorf("mjpeg 编码参数错误: %v", args[:2])
	}
	if !contains(args, "-q:v") || !contains(args, "pipe:1") {
		t.Errorf("mjpg 参数缺失: %v", args)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
