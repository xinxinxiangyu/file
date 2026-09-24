package fileio_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"example.com/gofs/internal/fileio"
)

// shortWriter 前 remaining 次 Write 每次只写前若干字节且不报错，
// 用来在常规文件不发生短写时，人为制造「短写」。
type shortWriter struct {
	w         io.Writer
	remaining int
}

func (s *shortWriter) Write(p []byte) (int, error) {
	if s.remaining > 0 {
		k := min(len(p), 2)
		s.remaining--
		_, err := s.w.Write(p[:k])
		return k, err
	}
	return s.w.Write(p)
}

func TestWriteFullHandlesShortWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "short.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	data := []byte("0123456789")
	sw := &shortWriter{w: f, remaining: 3}
	n, err := fileio.WriteFull(sw, data)
	if err != nil {
		t.Fatalf("WriteFull: %v", err)
	}
	if n != len(data) {
		t.Fatalf("返回 n=%d, 期望 %d", n, len(data))
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("文件内容 %q, 期望 %q：短写循环必须保证字节顺序与数量正确", got, data)
	}
}

func TestWriteFullZeroProgress(t *testing.T) {
	_, err := fileio.WriteFull(zeroWriter{}, []byte("abc"))
	if err == nil {
		t.Fatal("对返回 (0, nil) 的 Writer 期望报错，得到 nil")
	}
}

type zeroWriter struct{}

func (zeroWriter) Write(p []byte) (int, error) { return 0, nil }

func TestEOFIsNotTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refresh.txt")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	buf := make([]byte, 3)
	if _, err := io.ReadFull(a, buf); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("期望偏移到末尾时得到 io.EOF，得到 %v", err)
	}

	// 另一个句柄在末尾追加（注意不能用 os.WriteFile：它带 O_TRUNC）。
	b, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileio.WriteFull(b, []byte("-two")); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	// A 不重新打开，偏移还停在旧 EOF：文件变长后旧 EOF 不再是 EOF。
	more := make([]byte, 4)
	n, err := a.Read(more)
	if err != nil {
		t.Fatalf("追加后期望读到新字节，得到 err=%v", err)
	}
	if string(more[:n]) != "-two" {
		t.Fatalf("读到 %q, 期望 -two", more[:n])
	}
}

func TestOAppendPreservesAndOTruncClears(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flags.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	ap, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileio.WriteFull(ap, []byte("-more")); err != nil {
		t.Fatal(err)
	}
	if err := ap.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep-more" {
		t.Fatalf("O_APPEND 后内容 %q, 期望 keep-more", got)
	}

	tp, err := os.OpenFile(path, os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if err := tp.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("O_TRUNC 后大小 %d, 期望 0", info.Size())
	}
}

func TestReadAtDoesNotMoveOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readat.txt")
	if err := os.WriteFile(path, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	at := make([]byte, 3)
	if _, err := f.ReadAt(at, 4); err != nil {
		t.Fatal(err)
	}
	if string(at) != "efg" {
		t.Fatalf("ReadAt(4) = %q, 期望 efg", at)
	}

	// ReadAt 不动共享偏移：句柄刚打开，偏移仍是 0。
	head := make([]byte, 3)
	if _, err := io.ReadFull(f, head); err != nil {
		t.Fatal(err)
	}
	if string(head) != "abc" {
		t.Fatalf("ReadAt 后普通 Read = %q, 期望 abc（偏移被移动了）", head)
	}
}
