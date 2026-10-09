// Command finfo 是第 2 课的演示程序：像 ls -l 一样解读文件的「档案」。
//
//	go run ./cmd/finfo [路径]        路径是目录则列出其中条目，是文件则显示一行
//	go run ./cmd/finfo -stat 路径    额外打印 inode、设备号、链接数等底层字段
//	go run ./cmd/finfo -octal 路径   只打印八进制权限（如 0644）
//
// 不带参数时路径默认为当前目录。注意这里用 Lstat：看名字本身、不跟随
// 符号链接（第 5 课细讲 Stat/Lstat 的区别）。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"syscall"

	"example.com/gofs/internal/finfo"
)

func main() {
	log.SetFlags(0)

	statFlag := flag.Bool("stat", false, "打印 inode/设备号等底层 stat 字段")
	octalFlag := flag.Bool("octal", false, "只打印八进制权限")
	flag.Parse()

	path := flag.Arg(0)
	if path == "" {
		path = "."
	}

	info, err := os.Lstat(path)
	if err != nil {
		fatalStat(path, err)
	}

	if *octalFlag {
		fmt.Printf("%04o %s\n", finfo.OctalMode(info.Mode()), path)
		return
	}

	fmt.Println(finfo.Describe(info))
	if *statFlag {
		printSys(info)
	}

	// 目录：逐条 Lstat 列出内容（ReadDir 返回的 DirEntry.Info 即 lstat 结果，
	// 不会把符号链接误显示成目标文件）。
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			fatalStat(path, err)
		}
		for _, e := range entries {
			ei, err := e.Info()
			if err != nil {
				log.Fatalf("读取条目 %s 的信息失败: %v", e.Name(), err)
			}
			fmt.Println(finfo.Describe(ei))
		}
	}
}

// printSys 打印 FileInfo 背后的 syscall.Stat_t：这些字段 fs.FileInfo
// 接口没有直接暴露，但 ls -l 的链接数、属主等信息都来自这里。
func printSys(info fs.FileInfo) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		log.Fatal("Sys() 不是 *syscall.Stat_t：当前平台不受支持")
	}
	fmt.Printf("    inode=%d 设备 dev=%d rdev=%d 原始模式=%#o 硬链接数=%d UID=%d GID=%d\n",
		st.Ino, st.Dev, st.Rdev, st.Mode, st.Nlink, st.Uid, st.Gid)
}

// fatalStat 用哨兵错误区分「不存在」和「权限被拒绝」，
// 而不是把错误字符串拿来匹配。
func fatalStat(path string, err error) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		log.Fatalf("%s: 不存在（fs.ErrNotExist）", path)
	case errors.Is(err, fs.ErrPermission):
		log.Fatalf("%s: 权限被拒绝（fs.ErrPermission）", path)
	default:
		log.Fatalf("stat %s: %v", path, err)
	}
}
