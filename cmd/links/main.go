// Command links 是第 5 课的演示程序：在一个沙盒目录里完整走一遍
// 硬链接、符号链接、文件身份、原子 rename，最后用 inode 去重给 du 补课。
//
//	go run ./cmd/links [目录]     目录默认 sandbox；开始前清空、结束后删除
//
// 每一步都把 inode 号和链接数打印出来——这一课的所有结论都能从
// Stat_t 的 (Dev, Ino, Nlink) 三个数字里直接看到。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"example.com/gofs/internal/finfo"
	"example.com/gofs/internal/linkops"
	"example.com/gofs/internal/walkx"
)

func main() {
	log.SetFlags(0)
	flag.Parse()

	target := flag.Arg(0)
	if target == "" {
		target = "sandbox"
	}
	run(target)
}

func run(root string) {
	// 干净开局：沙盒已存在就整个清掉（里面可能有上次留下的悬空链接）。
	if err := os.RemoveAll(root); err != nil {
		log.Fatalf("清理旧沙盒: %v", err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatalf("MkdirAll: %v", err)
	}

	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	clink := filepath.Join(root, "c.lnk")

	// ---- 步骤 1：创建普通文件，记下它的身份 -------------------------------
	const body = "一个 inode，可以有多个名字\n"
	if err := os.WriteFile(a, []byte(body), 0o644); err != nil {
		log.Fatalf("写 a.txt: %v", err)
	}
	ia := mustStat(a)
	dev, ino := linkops.Identity(ia)
	fmt.Printf("create   %-22s ino=%d nlink=%d mode=%s\n",
		a, ino, linkops.Nlink(ia), finfo.FormatMode(ia.Mode()))
	fmt.Printf("         （设备号 dev=%d：inode 号只在同一设备内有效）\n\n", dev)

	// ---- 步骤 2：硬链接——同一个 inode 的第二个名字 ------------------------
	if err := linkops.Hard(a, b); err != nil {
		log.Fatalf("Hard: %v", err)
	}
	ib := mustStat(b)
	_, inoB := linkops.Identity(ib)
	fmt.Printf("hardlink %-22s ino=%d nlink=%d   ← 同一个 inode、链接数 +1\n",
		b, inoB, linkops.Nlink(ib))
	same, err := linkops.SameFile(a, b)
	if err != nil {
		log.Fatalf("SameFile: %v", err)
	}
	fmt.Printf("         SameFile(a.txt, b.txt) = %v\n\n", same)

	// ---- 步骤 3：硬链接不是复制——从 b 写，从 a 读 -------------------------
	if err := os.WriteFile(b, []byte("从 b.txt 重写\n"), 0o644); err != nil {
		log.Fatalf("写 b.txt: %v", err)
	}
	got, err := os.ReadFile(a)
	if err != nil {
		log.Fatalf("读 a.txt: %v", err)
	}
	fmt.Printf("write b.txt → 读 a.txt 得到 %q——内容联动，它们本就是同一个文件\n\n", got)

	// ---- 步骤 4：符号链接——内容是路径字符串的独立小文件 -------------------
	if err := linkops.Soft("a.txt", clink); err != nil {
		log.Fatalf("Soft: %v", err)
	}
	lc := mustLstat(clink)
	_, inoLink := linkops.Identity(lc)
	fmt.Printf("symlink  %-22s -> a.txt（相对路径，相对链接所在目录解析）\n", clink)
	fmt.Printf("lstat    %-22s mode=%s ino=%d（链接自己的 inode，≠ %d）size=%d（路径串长度）\n",
		clink, finfo.FormatMode(lc.Mode()), inoLink, ino, lc.Size())
	tc := mustStat(clink)
	_, inoFollow := linkops.Identity(tc)
	fmt.Printf("stat     %-22s 跟随链接 → ino=%d（就是 a.txt）\n", clink, inoFollow)
	target, err := os.Readlink(clink)
	if err != nil {
		log.Fatalf("Readlink: %v", err)
	}
	fmt.Printf("readlink %-22s → %q（链接文件里存的原始字符串）\n\n", clink, target)

	// ---- 步骤 5：删除一个名字——数据只在 nlink 归零时释放 ------------------
	if err := os.Remove(a); err != nil {
		log.Fatalf("Remove(a.txt): %v", err)
	}
	ib = mustStat(b)
	fmt.Printf("remove a.txt：b.txt 仍在！ino=%d nlink=%d，内容 %q\n",
		inoB, linkops.Nlink(ib), mustRead(b))
	dangling, err := linkops.IsDangling(clink)
	if err != nil {
		log.Fatalf("IsDangling: %v", err)
	}
	fmt.Printf("         c.lnk 此刻 dangling=%v——它存的是名字 \"a.txt\"，名字没了\n\n", dangling)

	// ---- 步骤 6：rename 改的是目录项，inode 不动 --------------------------
	d := filepath.Join(root, "d.txt")
	if err := os.Rename(b, d); err != nil {
		log.Fatalf("Rename b.txt d.txt: %v", err)
	}
	id := mustStat(d)
	_, inoD := linkops.Identity(id)
	fmt.Printf("rename b.txt -> d.txt：ino=%d（还是 %d），内容 %q\n",
		inoD, inoB, mustRead(d))
	fmt.Println("         rename 只是在目录表里把一条记录改 key，inode 和数据纹丝不动")
	if _, err := os.Lstat(b); err == nil {
		log.Fatalf("旧名字 b.txt 应已随 rename 消失")
	} else if !errors.Is(err, fs.ErrNotExist) {
		log.Fatalf("Lstat(b.txt): %v", err)
	}
	fmt.Println()

	// ---- 步骤 7：rename 的原子覆盖（第 6 课原子写的基石） -----------------
	cfg := filepath.Join(root, "config")
	tmp := filepath.Join(root, "config.tmp")
	mustWrite(cfg, "旧配置\n")
	mustWrite(tmp, "新配置\n")
	if err := os.Rename(tmp, cfg); err != nil {
		log.Fatalf("Rename 覆盖: %v", err)
	}
	fmt.Printf("rename config.tmp -> config（config 已存在）：读到 %q\n", mustRead(cfg))
	fmt.Println("         内核一步完成：外部任何时刻只可能看到旧文件或新文件，没有半截")
	fmt.Println("         同文件系统内才行；跨设备 rename 报 EXDEV，目录/文件类型互盖也报错")
	fmt.Println()

	// ---- 步骤 8：rename 让一个好端端的符号链接变悬空 -----------------------
	dlink := filepath.Join(root, "d.lnk")
	if err := linkops.Soft("d.txt", dlink); err != nil {
		log.Fatalf("Soft: %v", err)
	}
	fmt.Printf("symlink d.lnk -> d.txt，读 d.lnk 得到 %q\n", mustRead(dlink))
	d2 := filepath.Join(root, "d2.txt")
	if err := os.Rename(d, d2); err != nil {
		log.Fatalf("Rename: %v", err)
	}
	dangling, err = linkops.IsDangling(dlink)
	if err != nil {
		log.Fatalf("IsDangling: %v", err)
	}
	fmt.Printf("rename d.txt -> d2.txt 后：d.lnk dangling=%v（它记的是名字，不是 inode）\n", dangling)
	fmt.Printf("         数据完好地活在 d2.txt：%q\n\n", mustRead(d2))

	// ---- 步骤 9：硬链接的两条禁令 -----------------------------------------
	dir := filepath.Join(root, "adir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		log.Fatalf("Mkdir: %v", err)
	}
	err = linkops.Hard(dir, filepath.Join(root, "dir-alias"))
	fmt.Printf("给目录建硬链接：%v（内核拒绝——否则目录图会成环；'.' '..' 是仅有的例外）\n", err)

	// ---- 步骤 10：符号链接环——WalkDir 安全，EvalSymlinks 报 ELOOP --------
	x := filepath.Join(root, "x")
	y := filepath.Join(root, "y")
	if err := linkops.Soft("y", x); err != nil {
		log.Fatalf("Soft x: %v", err)
	}
	if err := linkops.Soft("x", y); err != nil {
		log.Fatalf("Soft y: %v", err)
	}
	if _, err := filepath.EvalSymlinks(x); err != nil {
		fmt.Printf("软链环 x->y->x：Lstat 正常（看链接本身），EvalSymlinks 报错：%v\n", err)
	}
	var walked int
	walkErr := filepath.WalkDir(root, func(_ string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		walked++
		return nil
	})
	if walkErr != nil {
		log.Fatalf("WalkDir 不应被软链环困住：%v", walkErr)
	}
	fmt.Printf("         WalkDir 遍历沙盒共 %d 个条目正常结束——它不跟随符号链接\n\n", walked)

	// ---- 步骤 11：给第 4 课的 du 补课：inode 去重 -------------------------
	demo := filepath.Join(root, "dudemo")
	if err := os.MkdirAll(demo, 0o755); err != nil {
		log.Fatalf("MkdirAll dudemo: %v", err)
	}
	data := filepath.Join(demo, "data.txt")
	h1 := filepath.Join(demo, "h1.txt")
	h2 := filepath.Join(demo, "h2.txt")
	other := filepath.Join(demo, "other.txt")
	sl := filepath.Join(demo, "data.lnk")
	mustWrite(data, "hard-linked payload\n") // 20 字节
	if err := linkops.Hard(data, h1); err != nil {
		log.Fatalf("Hard h1: %v", err)
	}
	if err := linkops.Hard(data, h2); err != nil {
		log.Fatalf("Hard h2: %v", err)
	}
	mustWrite(other, "unrelated\n") // 9 字节
	if err := linkops.Soft("data.txt", sl); err != nil {
		log.Fatalf("Soft data.lnk: %v", err)
	}

	naiveBytes, naiveFiles, err := walkx.DirSize(demo, false)
	if err != nil {
		log.Fatalf("DirSize naive: %v", err)
	}
	dedupBytes, dedupFiles, err := walkx.DirSize(demo, true)
	if err != nil {
		log.Fatalf("DirSize dedup: %v", err)
	}
	fmt.Println("dudemo/ 里 data.txt、h1.txt、h2.txt 是同一个 inode，另有 other.txt、一个软链：")
	fmt.Printf("du 第 4 课口径：%d 字节、%d 文件（硬链接计了 3 遍）\n", naiveBytes, naiveFiles)
	fmt.Printf("du -dedup 口径：%d 字节、%d 文件（(Dev,Ino) 去重，软链不计）\n",
		dedupBytes, dedupFiles)
	fmt.Println("         命令行对比两个口径：go run ./cmd/du [-dedup] 含硬链接的目录")
	fmt.Println()

	// ---- 收尾：删掉整个沙盒，最后一个名字消失，数据才真正释放 --------------
	if err := os.RemoveAll(root); err != nil {
		log.Fatalf("RemoveAll: %v", err)
	}
	_, err = os.Stat(root)
	fmt.Printf("RemoveAll(%s) 后 Stat：errors.Is(ErrNotExist) = %v——演示结束。\n",
		root, errors.Is(err, fs.ErrNotExist))
}

func mustStat(path string) os.FileInfo {
	info, err := os.Stat(path)
	if err != nil {
		log.Fatalf("Stat %s: %v", path, err)
	}
	return info
}

func mustLstat(path string) os.FileInfo {
	info, err := os.Lstat(path)
	if err != nil {
		log.Fatalf("Lstat %s: %v", path, err)
	}
	return info
}

func mustRead(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("ReadFile %s: %v", path, err)
	}
	return string(b)
}

func mustWrite(path, body string) {
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		log.Fatalf("写 %s: %v", path, err)
	}
}
