// ─────────────────────────────────────────────────────────────
// FasterEdge 开源项目
// Github: https://github.com/FasterEdge
// Gitee:  https://gitee.com/FasterEdge
// ─────────────────────────────────────────────────────────────
// Package mjpg 从 ffmpeg 的 MJPEG stdout 管道中切分出完整 JPEG 帧,
// 并以 multipart/x-mixed-replace 广播给任意数量的 HTTP 客户端。
package mjpg

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
)

const (
	boundary = "rtsp2other-frame"
	// maxFrame 防止异常数据导致的内存膨胀, 单帧超过即丢弃整帧。
	maxFrame = 64 << 20
)

// Hub 管理帧广播与订阅客户端。
type Hub struct {
	log     *slog.Logger
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	closed  bool
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{log: log, clients: make(map[chan []byte]struct{})}
}

// Subscribe 注册一个客户端, 返回帧通道与取消函数。
// 通道容量为 2, 客户端消费不及时会被丢弃(慢客户端保护)。
func (h *Hub) Subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 2)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.clients[ch]; ok {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// ClientCount 返回当前订阅客户端数量。
func (h *Hub) ClientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) broadcast(frame []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.clients) == 0 {
		return
	}
	// 必须拷贝: 调用方(切分器)会复用其内部缓冲。
	cp := make([]byte, len(frame))
	copy(cp, frame)
	for ch := range h.clients {
		select {
		case ch <- cp:
		default:
			// 慢客户端: 丢弃该帧
		}
	}
}

// Run 持续读取 r 并切分 JPEG 帧广播, 直到 EOF/错误。
func (h *Hub) Run(r interface{ Read([]byte) (int, error) }) error {
	st := &splitState{}
	buf := make([]byte, 256<<10)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			left := buf[:n]
			for len(left) > 0 {
				consumed := st.push(left, h.broadcast)
				left = left[consumed:]
			}
		}
		if err != nil {
			return err
		}
	}
}

// ServeHTTP 以 multipart/x-mixed-replace 提供 MJPEG 流。
func (h *Hub) ServeHTTP(w http.ResponseWriter, rr *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+boundary)
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ch, cancel := h.Subscribe()
	defer cancel()
	ctx := rr.Context()

	// 先写一个空帧头, 帮助部分浏览器建立连接
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", boundary, len(frame)); err != nil {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			if _, err := w.Write([]byte("\r\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// splitState 是 JPEG 帧切分状态机。
// 依据: JPEG 以 0xFFD8 起始, 以 0xFFD9 结束; 熵编码段内的 0xFF 会被 "0xFF 0x00" 字节填充转义,
// 因此 0xFFD9 不会在帧内数据中误出现。
type splitState struct {
	frame   []byte
	inFrame bool
	prevFF  bool // 上一个字节是 0xFF, 等待下一个字节确认标记
}

// push 处理 chunk, 完整的帧通过 emit 回调。返回本次消费的字节数。
// 调用方保证多次 push 之间状态连续。
func (s *splitState) push(chunk []byte, emit func([]byte)) int {
	i := 0
	for i < len(chunk) {
		b := chunk[i]
		if s.prevFF {
			s.prevFF = false
			switch b {
			case 0xFF:
				// 连续 FF: 仍可能是标记前缀, 保留
				s.prevFF = true
				i++
				continue
			case 0xD8:
				// SOI: 开始新帧(若已在帧内则视为数据异常, 重置)
				s.frame = s.frame[:0]
				s.frame = append(s.frame, 0xFF, 0xD8)
				s.inFrame = true
				i++
				continue
			case 0xD9:
				// EOI: 结束帧
				if s.inFrame {
					s.frame = append(s.frame, 0xFF, 0xD9)
					s.emit(emit)
				}
				// 未在帧内的孤立 EOI 直接忽略
				i++
				continue
			default:
				// 其他标记字节
				if s.inFrame {
					s.frame = append(s.frame, 0xFF, b)
					s.checkSize()
				}
				// 未在帧内: 忽略(垃圾数据)
				i++
				continue
			}
		}
		if b == 0xFF {
			s.prevFF = true
			i++
			continue
		}
		if s.inFrame {
			s.frame = append(s.frame, b)
			s.checkSize()
		}
		i++
	}
	// 返回消费字节数 = len(chunk)
	return len(chunk)
}

func (s *splitState) emit(emit func([]byte)) {
	emit(s.frame)
	s.frame = s.frame[:0]
	s.inFrame = false
}

func (s *splitState) checkSize() {
	if len(s.frame) > maxFrame {
		// 异常超大帧: 丢弃并复位
		s.frame = s.frame[:0]
		s.inFrame = false
	}
}
