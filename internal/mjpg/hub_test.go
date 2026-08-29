package mjpg

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
)

// 构造一个合法的最小 JPEG 帧: FF D8 ... FF D9
// 数据字节刻意避开 0xFF/0xD9, 模拟真实 JPEG 熵编码数据的字节填充行为。
func makeJPEG(seed byte, size int) []byte {
	b := make([]byte, 0, size+4)
	b = append(b, 0xFF, 0xD8)
	for i := 0; i < size; i++ {
		b = append(b, byte(seed)+byte(i%200))
	}
	// 数据中夹带需要正确跳过的 0xFF 单字节(与 0x00 配对转义), 防止误判
	b = append(b, 0xFF, 0x00)
	b = append(b, 0xFF, 0xD9)
	return b
}

func TestSplitterSingleFrame(t *testing.T) {
	st := &splitState{}
	var frames [][]byte
	f := makeJPEG(1, 100)
	st.push(f, func(b []byte) { frames = append(frames, b) })
	if len(frames) != 1 {
		t.Fatalf("期望 1 帧, 得到 %d", len(frames))
	}
	if !bytes.Equal(frames[0], f) {
		t.Fatal("帧内容不一致")
	}
}

func TestSplitterMultipleFramesChunked(t *testing.T) {
	st := &splitState{}
	var frames [][]byte
	f1 := makeJPEG(1, 100)
	f2 := makeJPEG(2, 50)
	f3 := makeJPEG(3, 200)
	all := append(append(append([]byte{}, f1...), f2...), f3...)

	// 随机切块喂入
	for i := 0; i < len(all); i += 7 {
		end := i + 7
		if end > len(all) {
			end = len(all)
		}
		st.push(all[i:end], func(b []byte) {
			cp := make([]byte, len(b))
			copy(cp, b) // 注意: 切分器复用内部缓冲, 必须拷贝
			frames = append(frames, cp)
		})
	}
	if len(frames) != 3 {
		t.Fatalf("期望 3 帧, 得到 %d", len(frames))
	}
	if !bytes.Equal(frames[0], f1) || !bytes.Equal(frames[1], f2) || !bytes.Equal(frames[2], f3) {
		t.Fatal("帧切分内容错误")
	}
}

func TestSplitterHandleStrayBytes(t *testing.T) {
	st := &splitState{}
	var frames [][]byte
	emit := func(b []byte) { frames = append(frames, b) }
	// 帧前的垃圾数据应被忽略
	st.push([]byte{0x00, 0x01, 0x02, 0xAA, 0xBB}, emit)
	f := makeJPEG(9, 64)
	st.push(f, emit)
	st.push([]byte{0xEE, 0xFF, 0x00, 0xAA}, emit) // 帧后垃圾
	if len(frames) != 1 {
		t.Fatalf("期望 1 帧, 得到 %d", len(frames))
	}
	if !bytes.Equal(frames[0], f) {
		t.Fatal("帧内容错误")
	}
}

func TestHubBroadcastAndSlowClient(t *testing.T) {
	h := NewHub(testLogger())
	f1 := makeJPEG(1, 32)
	f2 := makeJPEG(2, 32)

	ch1, cancel1 := h.Subscribe()
	ch2, cancel2 := h.Subscribe()
	defer cancel1()
	defer cancel2()

	// 慢客户端: 不读取 ch2
	_ = ch2

	// 广播应非阻塞, 慢客户端帧被丢弃但不影响快客户端
	h.broadcast(f1)
	h.broadcast(f2)

	got := <-ch1 // 快客户端能收到
	if !bytes.Equal(got, f1) && !bytes.Equal(got, f2) {
		t.Fatal("收到的帧内容不符")
	}
	if got := h.ClientCount(); got != 2 {
		t.Fatalf("期望 2 客户端, 得到 %d", got)
	}
}

func TestHubRunPipe(t *testing.T) {
	h := NewHub(testLogger())
	var f1, f2 []byte
	f1 = makeJPEG(1, 64)
	f2 = makeJPEG(2, 64)
	stream := append(append([]byte{}, f1...), f2...)

	ch, cancel := h.Subscribe()
	defer cancel()

	go func() {
		_ = h.Run(bytes.NewReader(stream))
	}()

	got1 := <-ch
	got2 := <-ch
	if !bytes.Equal(got1, f1) || !bytes.Equal(got2, f2) {
		t.Fatal("通过管道切分帧失败")
	}
	_ = io.EOF
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
