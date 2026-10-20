package linkops_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"example.com/gofs/internal/linkops"
)

// stat 是测试里反复要用的 Lstat/Stat 简写。
func lstat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("Lstat %s: %v", path, err)
	}
	return info
}

func stat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s: %v", path, err)
	}
	return info
}

// 硬链接：两个名字共享同一个 inode，nlink 从 1 变 2；
// 两个名字的 (Dev, Ino) 身份完全相同。
func TestHardLinkSameIdentity(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	if err := os.WriteFile(a, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := linkops.Hard(a, b); err != nil {
		t.Fatalf("Hard: %v", err)
	}

	ia, ib := lstat(t, a), lstat(t, b)
	devA, inoA := linkops.Identity(ia)
	devB, inoB := linkops.Identity(ib)
	if devA != devB || inoA != inoB {
		t.Fatalf("硬链接身份应相同：a=(%d,%d) b=(%d,%d)", devA, inoA, devB, inoB)
	}
	if n := linkops.Nlink(ia); n != 2 {
		t.Errorf("建硬链接后 nlink = %d，期望 2", n)
	}
	if n := linkops.Nlink(ib); n != 2 {
		t.Errorf("从 b 一侧看 nlink = %d，期望 2（nlink 存在 inode 上，不分名字）", n)
	}

	same, err := linkops.SameFile(a, b)
	if err != nil {
		t.Fatalf("SameFile: %v", err)
	}
	if !same {
		t.Error("SameFile(a, b) = false，期望 true")
	}
}

// 硬链接不是复制：从任一名字写入，另一个名字立刻看到同样的内容。
func TestHardLinkContentShared(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	if err := os.WriteFile(a, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkops.Hard(a, b); err != nil {
		t.Fatalf("Hard: %v", err)
	}

	// 从 b 截断重写；再从 a 读——它们本来就是同一个 inode、同一组数据块。
	if err := os.WriteFile(b, []byte("rewritten via b"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "rewritten via b" {
		t.Errorf("从 a 读到 %q，期望 %q（内容应联动）", got, "rewritten via b")
	}
}

// rm 只是删掉一个名字：原文件有硬链接时，删除原名后数据仍在，
// nlink 掉回 1；直到最后一个名字被删，Stat 才报 ErrNotExist。
func TestRemoveOriginalKeepsHardLink(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	if err := os.WriteFile(a, []byte("survive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkops.Hard(a, b); err != nil {
		t.Fatalf("Hard: %v", err)
	}

	if err := os.Remove(a); err != nil {
		t.Fatalf("Remove(a): %v", err)
	}
	if _, err := os.Stat(a); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a 应已不存在，得到 %v", err)
	}

	ib := stat(t, b)
	if n := linkops.Nlink(ib); n != 1 {
		t.Errorf("删掉 a 后 b 的 nlink = %d，期望 1", n)
	}
	got, err := os.ReadFile(b)
	if err != nil {
		t.Fatalf("a 被删后读 b: %v", err)
	}
	if string(got) != "survive" {
		t.Errorf("b 的内容 = %q，期望 %q（数据只在 nlink 归零时释放）", got, "survive")
	}

	// 删掉最后一个名字，文件才真正消失。
	if err := os.Remove(b); err != nil {
		t.Fatalf("Remove(b): %v", err)
	}
	if _, err := os.Stat(b); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("b 也删除后应不存在，得到 %v", err)
	}
}

// 硬链接不能给目录建——否则目录图会出现环，内核直接拒绝。
func TestHardLinkToDirFails(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	alias := filepath.Join(root, "dir-alias")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkops.Hard(dir, alias); err == nil {
		t.Fatal("给目录建硬链接应当失败（防止环）")
	}
}

// 硬链接的目标名字已存在时报错，link 不做覆盖。
func TestHardLinkExistingDstFails(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	if err := os.WriteFile(a, []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkops.Hard(a, b); err == nil {
		t.Fatal("dst 已存在时硬链接应失败")
	}
}

// 符号链接自己是 type=l 的独立 inode：Lstat 看链接、Stat 跟随到目标，
// 两者身份不同；Readlink 取回链接里存的路径字符串。
func TestSymlinkLstatVersusStat(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	link := filepath.Join(root, "c.lnk")
	if err := os.WriteFile(a, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkops.Soft("a.txt", link); err != nil {
		t.Fatalf("Soft: %v", err)
	}

	// Lstat 看链接本身：类型位带 ModeSymlink，大小是目标路径字符串长度。
	li := lstat(t, link)
	if li.Mode()&fs.ModeSymlink == 0 {
		t.Error("Lstat(链接) 的类型应为符号链接 l")
	}
	if li.Size() != int64(len("a.txt")) {
		t.Errorf("Lstat 大小 = %d，期望 %d（存的是路径字符串）", li.Size(), len("a.txt"))
	}

	// Stat 跟随链接，拿到的是 a.txt 的身份。
	ti := stat(t, link)
	if ti.Mode()&fs.ModeSymlink != 0 {
		t.Error("Stat(链接) 应跟随到常规文件，类型不再是 l")
	}
	devL, inoL := linkops.Identity(li)
	devT, inoT := linkops.Identity(ti)
	if devL == devT && inoL == inoT {
		t.Error("链接自身 inode 不应等于目标 inode")
	}

	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != "a.txt" {
		t.Errorf("Readlink = %q，期望 %q", target, "a.txt")
	}

	// SameFile 两边用 Stat：链接与目标读写同一份数据，判为同一文件。
	same, err := linkops.SameFile(a, link)
	if err != nil {
		t.Fatalf("SameFile: %v", err)
	}
	if !same {
		t.Error("SameFile(a.txt, c.lnk) 应为 true（Stat 都跟到 a.txt）")
	}
}

// 目标从来不存在也能建符号链接——创建时不解析目标，IsDangling 识别它。
func TestDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "dangling")
	if err := linkops.Soft("ghost", link); err != nil {
		t.Fatalf("Soft: %v", err)
	}

	dangling, err := linkops.IsDangling(link)
	if err != nil {
		t.Fatalf("IsDangling: %v", err)
	}
	if !dangling {
		t.Error("目标不存在的符号链接应判定为悬空")
	}
	// Lstat 成功（链接在）、Stat 失败（目标不在）——悬空的全部含义。
	_ = lstat(t, link)
	if _, err := os.Stat(link); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Stat 悬空链接应得 ErrNotExist，得到 %v", err)
	}

	// 把目标补上，链接立即「复活」，无需重建。
	if err := os.WriteFile(filepath.Join(root, "ghost"), []byte("born"), 0o644); err != nil {
		t.Fatal(err)
	}
	dangling, err = linkops.IsDangling(link)
	if err != nil {
		t.Fatalf("IsDangling: %v", err)
	}
	if dangling {
		t.Error("目标出现后不应再判定为悬空")
	}
}

// 目标被改名后，存着旧名字的符号链接变悬空；硬链接不受影响——
// 它指向 inode，符号链接指向名字。
func TestRenameTargetDanglesSymlink(t *testing.T) {
	root := t.TempDir()
	b := filepath.Join(root, "b.txt")
	link := filepath.Join(root, "c.lnk")
	if err := os.WriteFile(b, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := linkops.Soft("b.txt", link); err != nil {
		t.Fatalf("Soft: %v", err)
	}

	if err := os.Rename(b, filepath.Join(root, "d.txt")); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	dangling, err := linkops.IsDangling(link)
	if err != nil {
		t.Fatalf("IsDangling: %v", err)
	}
	if !dangling {
		t.Error("目标改名后，存旧名字的符号链接应变悬空（软链认名字）")
	}
}

// IsDangling 对各种非悬空输入都应返回 false。
func TestIsDanglingNegatives(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "f")
	dir := filepath.Join(root, "d")
	good := filepath.Join(root, "good.lnk")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := linkops.Soft("f", good); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{regular, dir, good} {
		dangling, err := linkops.IsDangling(p)
		if err != nil {
			t.Fatalf("IsDangling(%s): %v", p, err)
		}
		if dangling {
			t.Errorf("IsDangling(%s) = true，期望 false", filepath.Base(p))
		}
	}

	// 路径整体不存在：不是悬空，而是调用方错误。
	if _, err := linkops.IsDangling(filepath.Join(root, "no-such-path")); err == nil {
		t.Error("路径不存在时应返回错误，而不是悬空 false")
	}
}

// 相对符号链接相对「链接所在目录」解析：在子目录里建指向 ../a.txt 的链接，
// 同样能读到 root 下的 a.txt。
func TestSymlinkAcrossDirectories(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("upstairs"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sub, "up.lnk")
	if err := linkops.Soft(filepath.Join("..", "a.txt"), link); err != nil {
		t.Fatalf("Soft: %v", err)
	}

	got, err := os.ReadFile(link) // 内核顺着 sub/../a.txt 解析
	if err != nil {
		t.Fatalf("经跨目录相对软链读取: %v", err)
	}
	if string(got) != "upstairs" {
		t.Errorf("读到 %q，期望 upstairs", got)
	}
}

// Rename 是原子的且会覆盖：目标位置已存在的普通文件被整个替换，
// 外部任何时刻看到的都是旧文件或新文件之一，不会有半截内容。
func TestRenameAtomicOverwrite(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "config")
	tmp := filepath.Join(root, "config.tmp")
	if err := os.WriteFile(cfg, []byte("old config"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("new config"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(tmp, cfg); err != nil {
		t.Fatalf("Rename 覆盖: %v", err)
	}
	got, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new config" {
		t.Errorf("Rename 覆盖后读到 %q，期望 new config", got)
	}
	if _, err := os.Lstat(tmp); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("旧路径 tmp 应已随 rename 消失，得到 %v", err)
	}
}

// Rename 不能用文件覆盖目录、也不能用目录覆盖文件：类型冲突报错，
// 两个名字都保持原样（原子性意味着不会留下半完成状态）。
func TestRenameTypeConflictFails(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f")
	dir := filepath.Join(root, "d")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(file, dir); err == nil {
		t.Fatal("用文件覆盖目录应当失败")
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "x" {
		t.Errorf("失败后源文件应原样保留，得到 %q, %v", got, err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("失败后目标目录应原样保留，得到 %v, %v", fi, err)
	}
}

// Nlink 为 2 的文件与一个无关文件，SameFile 必须判否——
// 身份比较不能只看路径前缀或大小这种表面属性。
func TestSameFileRejectsUnrelated(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	for p, body := range map[string]string{a: "same body", b: "same body"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	same, err := linkops.SameFile(a, b)
	if err != nil {
		t.Fatalf("SameFile: %v", err)
	}
	if same {
		t.Error("内容相同的两个独立 inode 不应判为同一文件")
	}
}
