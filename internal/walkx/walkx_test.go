package walkx_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"example.com/gofs/internal/walkx"
)

// files 是测试树里的常规文件（相对 root 的路径 → 内容），
// 大小即内容字节数，DirSize 的期望值直接由它算出。
var files = map[string]string{
	"a.txt":               "hello", // 5
	"c.md":                "# c",   // 3
	"sub/b.md":            "go",    // 2
	"sub/deep/nested.log": "log",   // 3
}

// 额外的空目录：验证空目录在 tree 里出现、在 du 里不贡献字节。
var dirs = []string{
	"sub",
	filepath.Join("sub", "deep"),
	filepath.Join("sub", "empty"),
}

const wantBytes = int64(5 + 3 + 2 + 3)
const wantFiles = int64(4)

// buildTree 在 t.TempDir() 下搭出固定测试树，返回其 root：
//
//	root/
//	├── a.txt
//	├── c.md
//	└── sub/
//	    ├── b.md
//	    ├── deep/
//	    │   └── nested.log
//	    └── empty/
func buildTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for rel, body := range files {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// 先钉住 filepath 纯字符串函数的行为——这是「能手算 Clean/Join」的那组事实。
func TestFilepathBasics(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Join 清理中间的 ..", filepath.Join("a", "b", "../c"), filepath.Join("a", "c")},
		{"Clean 折叠 ./ 和多余斜杠", filepath.Clean("a/./b//"), filepath.Clean("a/b")},
		{"Clean 开头的 .. 保留", filepath.Clean("../../a"), filepath.Join("..", "..", "a")},
		{"Base 取最后一段", filepath.Base("/a/b.md"), "b.md"},
		{"Dir 取所在目录", filepath.Dir("/a/b.md"), string(os.PathSeparator) + "a"},
		{"Ext 取扩展名", filepath.Ext("/a/b.md"), ".md"},
		{"Rel 算相对路径", relOrDie(t, filepath.Join("root", "a"), filepath.Join("root", "a", "b", "c")), filepath.Join("b", "c")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("得到 %q，期望 %q", c.got, c.want)
			}
		})
	}
}

func relOrDie(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	return rel
}

func TestTree(t *testing.T) {
	root := buildTree(t)

	var buf bytes.Buffer
	if err := walkx.Tree(&buf, root); err != nil {
		t.Fatalf("Tree: %v", err)
	}

	want := filepath.Clean(root) + "\n" +
		"├── a.txt\n" +
		"├── c.md\n" +
		"└── sub\n" +
		"    ├── b.md\n" +
		"    ├── deep\n" +
		"    │   └── nested.log\n" +
		"    └── empty\n"

	if buf.String() != want {
		t.Fatalf("Tree 输出不符。\n得到：\n%s\n期望：\n%s", buf.String(), want)
	}
}

// Tree 的行数 = 条目总数（root + 文件 + 空目录都算）。
func TestTreeLineCount(t *testing.T) {
	root := buildTree(t)

	var buf bytes.Buffer
	if err := walkx.Tree(&buf, root); err != nil {
		t.Fatalf("Tree: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	// 1 root + 4 文件 + 3 目录
	if got := len(lines); got != 8 {
		t.Fatalf("Tree 行数 = %d，期望 8", got)
	}
}

func TestDirSize(t *testing.T) {
	root := buildTree(t)

	bytes, files, err := walkx.DirSize(root, false)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if bytes != wantBytes {
		t.Errorf("总字节 = %d，期望 %d", bytes, wantBytes)
	}
	if files != wantFiles {
		t.Errorf("文件数 = %d，期望 %d", files, wantFiles)
	}
}

// 第 5 课闭环：同一个 inode 有两个名字时，naive 口径计两遍，
// dedup 口径按 (Dev,Ino) 只计一遍。
func TestDirSizeHardLinkDedup(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt") // a.txt 的硬链接，同一个 inode
	other := filepath.Join(root, "c.txt")
	if err := os.WriteFile(a, []byte("hello"), 0o644); err != nil { // 5
		t.Fatal(err)
	}
	if err := os.Link(a, b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("go"), 0o644); err != nil { // 2
		t.Fatal(err)
	}

	naiveBytes, naiveFiles, err := walkx.DirSize(root, false)
	if err != nil {
		t.Fatalf("DirSize: %v", err)
	}
	if naiveBytes != 12 || naiveFiles != 3 {
		t.Errorf("不去重：得到 %d 字节/%d 文件，期望 12/3（硬链接计两遍）",
			naiveBytes, naiveFiles)
	}

	dedupBytes, dedupFiles, err := walkx.DirSize(root, true)
	if err != nil {
		t.Fatalf("DirSize(dedup): %v", err)
	}
	if dedupBytes != 7 || dedupFiles != 2 {
		t.Errorf("去重后：得到 %d 字节/%d 文件，期望 7/2（a 与 b 是同一文件）",
			dedupBytes, dedupFiles)
	}
}

// root 不存在：WalkDir 把错误放在第一次回调里，DirSize 返回错误与零值。
func TestDirSizeMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	bytes, files, err := walkx.DirSize(root, false)
	if err == nil {
		t.Fatal("不存在的 root 应返回错误")
	}
	if bytes != 0 || files != 0 {
		t.Errorf("错误时应为零值，得到 bytes=%d files=%d", bytes, files)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("期望 ErrNotExist，得到 %v", err)
	}
}

// 权限受限的子目录不能让遍历崩溃：兄弟文件照常汇总，错误经返回值带回。
func TestDirSizeUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 绕过权限检查，无法制造权限不足")
	}
	root := t.TempDir()
	secret := filepath.Join(root, "secret")
	if err := os.MkdirAll(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secret, "hidden.txt"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(secret, 0o755) }) // 否则 TempDir 清理会失败

	bytes, files, err := walkx.DirSize(root, false)
	if err == nil {
		t.Fatal("权限不足的子目录应产生错误")
	}
	if bytes != 2 || files != 1 {
		t.Errorf("应只统计到 ok.txt（2 字节/1 文件），得到 bytes=%d files=%d", bytes, files)
	}
}

func TestFindName(t *testing.T) {
	root := buildTree(t)

	m, err := walkx.MatchName("*.md")
	if err != nil {
		t.Fatalf("MatchName: %v", err)
	}
	got, err := walkx.Find(root, m)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	want := []string{
		filepath.Join(root, "c.md"),
		filepath.Join(root, "sub", "b.md"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Find *.md = %v，期望 %v", got, want)
	}
}

// 非法 glob 必须在构造 matcher 时就报错，而不是遍历之后。
func TestMatchNameBadPattern(t *testing.T) {
	if _, err := walkx.MatchName("["); err == nil {
		t.Fatal(`MatchName("[") 应返回 ErrBadPattern`)
	}
}

func TestFindTypeDir(t *testing.T) {
	root := buildTree(t)

	m, err := walkx.MatchType('d')
	if err != nil {
		t.Fatalf("MatchType: %v", err)
	}
	got, err := walkx.Find(root, m)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	want := []string{
		root,
		filepath.Join(root, "sub"),
		filepath.Join(root, "sub", "deep"),
		filepath.Join(root, "sub", "empty"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Find -type d = %v，期望 %v", got, want)
	}
}

func TestMatchTypeRejectsUnknownKind(t *testing.T) {
	if _, err := walkx.MatchType('x'); err == nil {
		t.Fatal("MatchType('x') 应报错")
	}
}

// 返回 fs.SkipDir 剪掉整棵 sub：sub 自己和它下面的一切都不出现。
func TestFindSkipDir(t *testing.T) {
	root := buildTree(t)

	pruneSub := func(path string, d fs.DirEntry) (bool, error) {
		if d.Name() == "sub" {
			return false, fs.SkipDir
		}
		return true, nil
	}
	got, err := walkx.Find(root, pruneSub)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	want := []string{
		root,
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "c.md"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("SkipDir 后 = %v，期望 %v", got, want)
	}
}

// WalkDir 不跟随符号链接：指向目录的链接只作为一个条目出现，
// 不会递归进目标（否则 deep/nested.log 会出现两次，目录环还会无限递归）。
func TestFindDoesNotFollowSymlink(t *testing.T) {
	root := buildTree(t)
	link := filepath.Join(root, "sub-link")
	if err := os.Symlink(filepath.Join(root, "sub"), link); err != nil {
		t.Fatal(err)
	}

	all := func(_ string, _ fs.DirEntry) (bool, error) { return true, nil }
	got, err := walkx.Find(root, all)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	var nestedCount int
	var hasLink bool
	for _, p := range got {
		if p == link {
			hasLink = true
		}
		if strings.HasSuffix(p, filepath.Join("deep", "nested.log")) {
			nestedCount++
		}
	}
	if !hasLink {
		t.Error("结果里应包含符号链接条目本身")
	}
	if nestedCount != 1 {
		t.Errorf("nested.log 出现 %d 次，期望 1 次（不跟随链接）", nestedCount)
	}
}

// 不存在的 root：Find 返回空结果 + ErrNotExist，而不是 panic。
func TestFindMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nope")
	all := func(_ string, _ fs.DirEntry) (bool, error) { return true, nil }
	got, err := walkx.Find(root, all)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("期望 ErrNotExist，得到 %v", err)
	}
	if len(got) != 0 {
		t.Errorf("错误时结果应为空，得到 %v", got)
	}
}
