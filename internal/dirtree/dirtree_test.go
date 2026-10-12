package dirtree_test

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"example.com/gofs/internal/dirtree"
)

func TestBuildLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sandbox")

	if err := dirtree.BuildLayout(root); err != nil {
		t.Fatalf("首次 BuildLayout: %v", err)
	}

	// 三个目录都真实存在且确实是目录。
	wantDirs := []string{"docs", filepath.Join("docs", "img"), "tmp"}
	for _, d := range wantDirs {
		info, err := os.Stat(filepath.Join(root, d))
		if err != nil {
			t.Errorf("Stat %s: %v", d, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s 应是目录", d)
		}
	}

	// readme.md 存在且有内容。
	body, err := os.ReadFile(filepath.Join(root, "docs", "readme.md"))
	if err != nil {
		t.Fatalf("读取 readme.md: %v", err)
	}
	if len(body) == 0 {
		t.Error("readme.md 不应为空")
	}

	// MkdirAll 的关键语义：对已存在的树再搭一次不报错（mkdir -p）。
	if err := dirtree.BuildLayout(root); err != nil {
		t.Fatalf("再次 BuildLayout 应幂等，得到 %v", err)
	}
}

// Mkdir 只建一级：中间路径缺失时报 ENOENT；MkdirAll 则一口气建完。
func TestMkdirOnlyOneLevel(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a", "b")

	err := os.Mkdir(path, 0o755)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Mkdir 缺失中间级：期望 ErrNotExist，得到 %v", err)
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll 应一次建两级: %v", err)
	}
}

func TestListContentAndOrder(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"c.txt", "a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := dirtree.List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Name()
	}
	want := []string{"a.txt", "b.txt", "c.txt", "d"}
	if !slices.Equal(got, want) {
		t.Fatalf("List 顺序 = %v，期望 %v（按名字排序）", got, want)
	}

	// DirEntry 不用 stat 就自带类型：最后一个 d 是目录，前三个不是。
	if !entries[3].IsDir() {
		t.Error("d 应被标记为目录")
	}
	if entries[0].IsDir() {
		t.Error("a.txt 不应被标记为目录")
	}
}

// DirEntry 是轻量的；需要大小时对具体条目调 Info() 做一次按需 stat。
func TestDirEntryInfoOnDemand(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sandbox")
	if err := dirtree.BuildLayout(root); err != nil {
		t.Fatal(err)
	}

	entries, err := dirtree.List(filepath.Join(root, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	var readme fs.DirEntry
	for _, e := range entries {
		if e.Name() == "readme.md" {
			readme = e
		}
	}
	if readme == nil {
		t.Fatal("docs 下没有找到 readme.md")
	}

	info, err := readme.Info()
	if err != nil {
		t.Fatalf("entry.Info(): %v", err)
	}
	st, err := os.Stat(filepath.Join(root, "docs", "readme.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != st.Size() {
		t.Errorf("Info() 大小 = %d，Stat 大小 = %d", info.Size(), st.Size())
	}
}

func TestTypeRune(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("f", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}

	entries, err := dirtree.List(dir)
	if err != nil {
		t.Fatal(err)
	}
	runes := make(map[string]byte, len(entries))
	for _, e := range entries {
		runes[e.Name()] = dirtree.TypeRune(e)
	}
	if runes["f"] != '-' {
		t.Errorf("常规文件类型字符 = %q，期望 '-'", runes["f"])
	}
	if runes["sub"] != 'd' {
		t.Errorf("目录类型字符 = %q，期望 'd'", runes["sub"])
	}
	if runes["link"] != 'l' {
		t.Errorf("符号链接类型字符 = %q，期望 'l'", runes["link"])
	}
}

func TestReaddirnames(t *testing.T) {
	dir := t.TempDir()
	want := []string{"a", "b", "c"}
	for _, name := range want {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got, err := f.Readdirnames(-1) // 只要名字，最轻量
	if err != nil {
		t.Fatalf("Readdirnames: %v", err)
	}
	slices.Sort(got) // 同样不保证顺序，断言前排序
	if !slices.Equal(got, want) {
		t.Fatalf("Readdirnames = %v，期望 %v", got, want)
	}
}

// Remove 只能删空目录或常规文件；非空目录必须 RemoveAll。
func TestRemoveRules(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := dirtree.BuildLayout(sub); err != nil {
		t.Fatal(err)
	}

	// 非空目录：Remove 失败（ENOTEMPTY；标准库没有可移植哨兵，只断言非 nil）。
	if err := os.Remove(sub); err == nil {
		t.Fatal("Remove 非空目录应失败，却成功了")
	}

	// 逐层清空：非空的 docs 用 RemoveAll，空的 tmp 用 Remove，
	// sub 变空后 Remove 成功。
	if err := os.RemoveAll(filepath.Join(sub, "docs")); err != nil {
		t.Fatalf("RemoveAll(docs): %v", err)
	}
	if err := os.Remove(filepath.Join(sub, "tmp")); err != nil {
		t.Fatalf("Remove(tmp): %v", err)
	}
	if err := os.Remove(sub); err != nil {
		t.Fatalf("sub 清空后 Remove: %v", err)
	}
}

func TestRemoveAll(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sandbox")
	if err := dirtree.BuildLayout(root); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("RemoveAll: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("RemoveAll 后 Stat：期望 ErrNotExist，得到 %v", err)
	}

	// 对不存在的路径 RemoveAll 也返回 nil，可放心重复调用。
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("对不存在的路径 RemoveAll 应幂等，得到 %v", err)
	}
}

// ReadDir(n) 的正数 n 用于分页：逐页读到 EOF，拼接结果应与一次读完一致。
func TestReadDirPaging(t *testing.T) {
	dir := t.TempDir()
	want := make([]string, 5)
	for i := range 5 {
		name := fmt.Sprintf("f%d", i)
		want[i] = name
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var got []string
	for {
		page, err := f.ReadDir(2)
		got = append(got, namesOf(page)...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("分页 ReadDir: %v", err)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("分页 ReadDir(2) 拼接 = %v，期望 %v", got, want)
	}
}

func namesOf(entries []fs.DirEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}
