package adapter

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestParseSSE(t *testing.T) {
	src := "event: a\ndata: line1\n\n" +
		"data: multi\ndata: line2\n\n" + // 多 data: 行以 \n 拼接
		"event: b\ndata: x\n\n"
	evs, err := ParseSSE(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	var got []SSEEvent
	for e := range evs {
		got = append(got, e)
	}
	if len(got) != 3 {
		t.Fatalf("事件数错误: %+v", got)
	}
	if got[0].Event != "a" || got[0].Data != "line1" {
		t.Fatalf("事件解析错误: %+v", got[0])
	}
	if got[1].Data != "multi\nline2" {
		t.Fatalf("多 data 行拼接错误: %q", got[1].Data)
	}
	if got[2].Event != "b" || got[2].Data != "x" {
		t.Fatalf("事件解析错误: %+v", got[2])
	}
}

// failingReader 读到若干字节后注入错误，验证 ParseSSE 上报而非静默结束。
type failingReader struct {
	src string
	off int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.off >= len(r.src) {
		return 0, errors.New("boom: 底层读错误")
	}
	n := copy(p, r.src[r.off:])
	r.off += n
	return n, nil
}

func TestParseSSEReportsReaderError(t *testing.T) {
	evs, err := ParseSSE(&failingReader{src: "data: partial\n"})
	if err != nil {
		t.Fatal(err)
	}
	sawErr := false
	for e := range evs {
		if e.Error != nil {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("底层读错误必须上报（不得静默截断输出）")
	}
}

var _ io.Reader = (*failingReader)(nil)
