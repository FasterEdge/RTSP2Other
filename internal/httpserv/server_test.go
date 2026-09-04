// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
package httpserv

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FasterEdge/RTSP2Other/internal/config"
)

func TestPlayRefusedXSSInjection(t *testing.T) {
	s := New(config.HTTPConfig{Prefix: ""}, nil, nil, nil, nil)

	cases := []struct {
		name string
		path string
		want int
	}{
		{"合法 mjpg 流名", "/play/cam01.mjpg", http.StatusOK},
		{"合法 mp4 流名", "/play/a_b-c.mp4", http.StatusOK},
		{"合法 hls 流名", "/play/live.m3u8", http.StatusOK},
		// 反射型 XSS 载荷: 注入引号/属性/脚本标签
		{"引号注入", "/play/x%22onerror%3Dalert(1).mjpg", http.StatusNotFound},
		{"标签注入", "/play/%3Cscript%3Ealert(1)%3C%2Fscript%3E.mjpg", http.StatusNotFound},
		{"尖括号", "/play/a%3Cb%3E.mjpg", http.StatusNotFound},
		{"空流名", "/play/", http.StatusNotFound},
		{"超长流名", "/play/" + strings.Repeat("a", 200) + ".mjpg", http.StatusNotFound},
		{"路径穿越片段", "/play/..%2Fetc.mjpg", http.StatusNotFound},
		{"百分号", "/play/50%25.mjpg", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("GET %s = %d, want %d", tc.path, rec.Code, tc.want)
			}
			// 被拒绝的请求, 响应体绝不能回显注入载荷。
			// (合法 HLS 播放页模板自身含 <script> 标签, 只对拒绝路径断言。)
			if tc.want == http.StatusNotFound {
				body := rec.Body.String()
				if strings.Contains(body, `<script>`) || strings.Contains(body, `onerror=`) {
					t.Fatalf("GET %s leaked injection into response: %s", tc.path, body)
				}
			}
		})
	}
}

func TestValidStreamName(t *testing.T) {
	ok := []string{"cam", "cam-01", "cam_01", "a.b", "x.mjpg", "A-Z_9"}
	bad := []string{"", "a b", "a\"b", "a<b", "a>b", "a&b", "a/b", "a\\b",
		"a?b", "a#b", "%", strings.Repeat("a", 129)}
	for _, s := range ok {
		if !validStreamName(s) {
			t.Errorf("validStreamName(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if validStreamName(s) {
			t.Errorf("validStreamName(%q) = true, want false", s)
		}
	}
}