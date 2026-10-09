package finfo_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"example.com/gofs/internal/finfo"
)

// makeFile 在临时目录里建一个文件并强制改成指定权限，返回它的 FileInfo。
func makeFile(t *testing.T, name string, perm fs.FileMode) os.FileInfo {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestFormatMode(t *testing.T) {
	// 这些是普通用户 chmod 能真正设上的 rwx 位，用真实文件 round-trip 验证。
	cases := []struct {
		perm fs.FileMode
		want string
	}{
		{0o644, "-rw-r--r--"},
		{0o755, "-rwxr-xr-x"},
		{0o600, "-rw-------"},
		{0o750, "-rwxr-x---"},
		{0o000, "----------"},
	}
	for _, c := range cases {
		info := makeFile(t, "f", c.perm)
		if got := finfo.FormatMode(info.Mode()); got != c.want {
			t.Errorf("Chmod(%#o) 的权限串 = %q, 期望 %q", c.perm, got, c.want)
		}
	}
}

func TestFormatModeSpecialBits(t *testing.T) {
	// setuid/setgid/sticky 的渲染只取决于 Mode 的高位，用直接构造的
	// fs.FileMode 验证，不经过真实 Chmod：在 macOS 上非 root 调 chmod
	// 设置这些位会被内核静默清除，没法用真实文件 round-trip。
	cases := []struct {
		mode fs.FileMode
		want string
	}{
		{fs.ModeSetuid | 0o755, "-rwsr-xr-x"},
		{fs.ModeSetgid | 0o755, "-rwxr-sr-x"},
		{fs.ModeSticky | 0o755, "-rwxr-xr-t"},
		{fs.ModeSetuid | 0o644, "-rwSr--r--"}, // 有 setuid 但属主无 x → 大写 S
		{fs.ModeSticky | 0o777, "-rwxrwxrwt"},
	}
	for _, c := range cases {
		if got := finfo.FormatMode(c.mode); got != c.want {
			t.Errorf("FormatMode(%v) = %q, 期望 %q", c.mode, got, c.want)
		}
	}
}

func TestFormatModeDir(t *testing.T) {
	// 显式以 0700 建一个目录（0700 不含组/其他位，不受 umask 影响）。
	path := filepath.Join(t.TempDir(), "d")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := finfo.FormatMode(info.Mode()); got != "drwx------" {
		t.Fatalf("0700 目录权限串 = %q, 期望 drwx------", got)
	}
}

func TestOctalMode(t *testing.T) {
	cases := []struct {
		mode fs.FileMode
		want uint32
	}{
		{0o644, 0o644},
		{0o755, 0o755},
		{0o000, 0o000},
		{fs.ModeSetuid | 0o755, 0o4755}, // 特殊位折叠回 04000
		{fs.ModeSticky | 0o777, 0o1777},
	}
	for _, c := range cases {
		if got := finfo.OctalMode(c.mode); got != c.want {
			t.Errorf("OctalMode(%v) = %#o, 期望 %#o", c.mode, got, c.want)
		}
	}
}

// 顺带把「macOS 非 root 无法用 chmod 设特殊位」这个平台事实钉死：
// 如果未来平台行为变化，这个测试会提醒我们更新讲解。
// （Linux 上普通用户可以设置这些位，故仅在 darwin 验证。）
func TestChmodStripsSpecialBitsForNormalUser(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("仅验证 macOS 的行为")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 可以设置 setuid/setgid/sticky")
	}
	info := makeFile(t, "special", 0o755|0o4000|0o2000|0o1000)
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("macOS 非 root chmod 后期望特殊位被清除、留下 0755，得到 %#o", got)
	}
}

func TestChtimesChangesModTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ts.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, target, target); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// 部分文件系统时间戳精度只有秒或微秒，截到秒再比。
	if got := info.ModTime().Truncate(time.Second); !got.Equal(target) {
		t.Fatalf("Chtimes 后 ModTime = %v, 期望 %v", got, target)
	}
}

func TestDescribeLooksLikeLsL(t *testing.T) {
	info := makeFile(t, "desc.txt", 0o644)
	line := finfo.Describe(info)

	for _, want := range []string{"-rw-r--r--", "desc.txt"} {
		if !strings.Contains(line, want) {
			t.Fatalf("Describe 输出 %q，其中应包含 %q", line, want)
		}
	}
}

func TestNotExistSentinel(t *testing.T) {
	_, err := os.Stat(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat 不存在的文件：errors.Is(err, ErrNotExist) = false，错误为 %v", err)
	}
}

func TestPermissionDeniedSentinel(t *testing.T) {
	// root 绕过一切权限检查，这个实验对 root 不成立。
	if os.Geteuid() == 0 {
		t.Skip("以 root 运行时 DAC 权限检查被绕过")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "locked.txt")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	// t.TempDir 的 RemoveAll 只需要目录的写权限即可 unlink，
	// 但仍在测试结束前恢复权限，保持临时目录干净可读。
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	f, err := os.Open(path)
	if err == nil {
		f.Close()
		t.Fatal("打开 0000 权限的文件期望被拒绝，却成功了")
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("打开 0000 文件：errors.Is(err, ErrPermission) = false，错误为 %v", err)
	}
}
