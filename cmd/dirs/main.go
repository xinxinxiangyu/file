// Command dirs 是第 3 课的演示程序：亲手建目录、列条目、
// 切换工作目录，再按规则删除。
//
//	go run ./cmd/dirs [目录]        在指定目录（默认 sandbox）里搭沙盒并完整演示
//	go run ./cmd/dirs -list [目录]  只列现有目录，不创建任何东西
//
// 演示开始前会把目标路径转成绝对路径：因为后面要 Chdir，Chdir 之后
// 原来的相对路径就不再指向原来的位置了。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"example.com/gofs/internal/dirtree"
)

func main() {
	log.SetFlags(0)

	listOnly := flag.Bool("list", false, "只列目录，不搭沙盒")
	flag.Parse()

	target := flag.Arg(0)
	if target == "" {
		target = "sandbox"
	}

	if *listOnly {
		runList(target)
		return
	}
	runBuild(target)
}

// runList 只列不建，对应 README 里的 `dirs -list demo`。
func runList(dir string) {
	entries, err := dirtree.List(dir)
	if err != nil {
		log.Fatalf("列目录 %s: %v", dir, err)
	}
	fmt.Printf("%s 共 %d 个条目（按名字排序）：\n", dir, len(entries))
	for _, e := range entries {
		fmt.Printf("  %c %s\n", dirtree.TypeRune(e), e.Name())
	}
}

// runBuild 搭沙盒并逐项演示：建 → 列 → 按需 stat → Chdir → 删。
func runBuild(target string) {
	root, err := filepath.Abs(target)
	if err != nil {
		log.Fatalf("取绝对路径: %v", err)
	}

	// ---- 步骤 1：MkdirAll 搭沙盒树 ---------------------------------------
	if err := dirtree.BuildLayout(root); err != nil {
		log.Fatalf("BuildLayout: %v", err)
	}
	fmt.Printf("已在 %s 搭好：docs/、docs/img/、docs/readme.md、tmp/\n\n", root)

	// ---- 步骤 2：ReadDir 列条目（类型字符 + 名字） ------------------------
	entries, err := dirtree.List(root)
	if err != nil {
		log.Fatalf("List(root): %v", err)
	}
	fmt.Println("ReadDir(root)：")
	for _, e := range entries {
		fmt.Printf("  %c %s\n", dirtree.TypeRune(e), e.Name())
	}

	docsEntries, err := dirtree.List(filepath.Join(root, "docs"))
	if err != nil {
		log.Fatalf("List(docs): %v", err)
	}
	fmt.Println("ReadDir(docs)：")
	for _, e := range docsEntries {
		fmt.Printf("  %c %s\n", dirtree.TypeRune(e), e.Name())
	}
	fmt.Println()

	// ---- 步骤 3：DirEntry 轻量，Info() 才按需 stat ------------------------
	var readme = mustFind(docsEntries, "readme.md")
	info, err := readme.Info()
	if err != nil {
		log.Fatalf("readme.Info(): %v", err)
	}
	fmt.Printf("列目录时只知道 readme.md 的名字和类型（%c）；调 Info() 才拿到 "+
		"大小 %d 字节、权限 %v——一次额外的 stat。\n\n",
		dirtree.TypeRune(readme), info.Size(), info.Mode().Perm())

	// ---- 步骤 4：三种列目录接口对比（都作用在打开的 *os.File 上） ----------
	compareListAPIs(filepath.Join(root, "docs"))

	// ---- 步骤 5：Getwd / Chdir -------------------------------------------
	oldWd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Getwd: %v", err)
	}
	fmt.Printf("Chdir 前工作目录：%s\n", oldWd)

	tmpDir := filepath.Join(root, "tmp")
	if err := os.Chdir(tmpDir); err != nil {
		log.Fatalf("Chdir(%s): %v", tmpDir, err)
	}
	now, _ := os.Getwd()
	fmt.Printf("Chdir 后工作目录：%s\n", now)

	// 用相对路径写文件，它落在新的工作目录里。
	if err := os.WriteFile("chdir.txt", []byte("相对路径写的\n"), 0o644); err != nil {
		log.Fatalf("相对路径 WriteFile: %v", err)
	}
	fmt.Printf("WriteFile(\"chdir.txt\") 实际落在 %s\n", filepath.Join(tmpDir, "chdir.txt"))

	// 必须切回去：Chdir 改的是整个进程的状态，不是这一个函数的。
	if err := os.Chdir(oldWd); err != nil {
		log.Fatalf("Chdir 回去: %v", err)
	}
	back, _ := os.Getwd()
	fmt.Printf("已 Chdir 回 %s——并发程序里应直接用绝对路径，别依赖 Chdir。\n\n", back)

	// ---- 步骤 6：Remove 的规则 -------------------------------------------
	if err := os.Remove(filepath.Join(tmpDir, "chdir.txt")); err != nil {
		log.Fatalf("Remove(chdir.txt): %v", err)
	}
	fmt.Println("Remove(chdir.txt)：常规文件直接删。")

	err = os.Remove(filepath.Join(root, "docs"))
	fmt.Printf("Remove(docs)（非空目录）：%v\n", err)
	fmt.Println("    —— 非空目录只能 RemoveAll；tmp 已空，Remove 可以：")
	if err := os.Remove(tmpDir); err != nil {
		log.Fatalf("Remove(tmp): %v", err)
	}
	fmt.Println("Remove(tmp)：空目录删除成功。")
	fmt.Println()

	// ---- 步骤 7：RemoveAll 递归收尾 ---------------------------------------
	if err := os.RemoveAll(root); err != nil {
		log.Fatalf("RemoveAll: %v", err)
	}
	_, err = os.Stat(root)
	fmt.Printf("RemoveAll(root) 后 Stat：%v\n", err)
	fmt.Printf("errors.Is(err, fs.ErrNotExist) = %v——沙盒已清空，演示结束。\n",
		errors.Is(err, fs.ErrNotExist))
}

// compareListAPIs 对同一个目录演示三种读法：DirEntry、只要名字、完整 FileInfo。
// 每次都重新 os.Open：目录句柄内部有读取位置，读过一遍不能从头再来。
func compareListAPIs(dir string) {
	fmt.Println("列目录的三种接口：")

	f, err := os.Open(dir)
	if err != nil {
		log.Fatalf("Open %s: %v", dir, err)
	}
	de, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		log.Fatalf("ReadDir: %v", err)
	}
	fmt.Printf("  ReadDir      → %d 个 DirEntry：%s（名字 + 类型，首选）\n",
		len(de), dirEntryNames(de))

	f, err = os.Open(dir)
	if err != nil {
		log.Fatalf("Open %s: %v", dir, err)
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		log.Fatalf("Readdirnames: %v", err)
	}
	fmt.Printf("  Readdirnames → %d 个字符串：%v（只要名字，最快）\n", len(names), names)

	f, err = os.Open(dir)
	if err != nil {
		log.Fatalf("Open %s: %v", dir, err)
	}
	fis, err := f.Readdir(-1)
	f.Close()
	if err != nil {
		log.Fatalf("Readdir: %v", err)
	}
	fmt.Printf("  Readdir      → %d 个 FileInfo（老接口，每条带 Size/权限，可能逐个 stat）\n",
		len(fis))
	for _, fi := range fis {
		fmt.Printf("                 %s  %d 字节\n", fi.Name(), fi.Size())
	}
	fmt.Println()
}

func mustFind(entries []os.DirEntry, name string) os.DirEntry {
	for _, e := range entries {
		if e.Name() == name {
			return e
		}
	}
	log.Fatalf("条目 %s 不存在", name)
	return nil
}

func dirEntryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}
