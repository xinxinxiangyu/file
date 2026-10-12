// Package dirtree 是第 3 课抽出的小工具包：在沙盒目录里搭布局、
// 排序列条目、把条目的类型位翻译成人能读的类型字符。
//
// 这一课的核心事实：目录本身是一张「名字 → inode」的映射表。
// 列目录（getdents）拿到的每条记录自带名字和类型，这就是轻量的
// DirEntry；只有需要大小、权限、时间戳时，才值得为某个条目额外
// 调一次 Info()（背后是 stat）。
package dirtree

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// layoutDirs 是 BuildLayout 要创建的目录（相对 root 的路径）。
var layoutDirs = []string{
	"docs",
	filepath.Join("docs", "img"),
	"tmp",
}

// readmeBody 是 docs/readme.md 的固定内容。
const readmeBody = "# 沙盒说明\n\n这棵目录树由 dirtree.BuildLayout 搭出，供第 3 课演示。\n"

// BuildLayout 在 root 下搭出固定的演示树：
//
//	root/
//	├── docs/
//	│   ├── img/
//	│   └── readme.md
//	└── tmp/
//
// root 不存在时会一并创建；全部用 MkdirAll，所以对已存在的目录
// 重复执行不报错（等价 mkdir -p），readme.md 已存在则被重写。
func BuildLayout(root string) error {
	for _, d := range layoutDirs {
		path := filepath.Join(root, d)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("MkdirAll %s: %w", path, err)
		}
	}
	readme := filepath.Join(root, "docs", "readme.md")
	if err := os.WriteFile(readme, []byte(readmeBody), 0o644); err != nil {
		return fmt.Errorf("写入 %s: %w", readme, err)
	}
	return nil
}

// List 用 ReadDir 列出目录的全部条目，并按名字升序排序。
//
// 注意这里刻意用 os.Open + f.ReadDir(-1)：包级函数 os.ReadDir 虽然
// 也返回 DirEntry，但它会替你排序；而 f.ReadDir 按文件系统的原始
// 目录顺序返回，不保证有序（很多文件系统是哈希序），所以自己排一遍，
// 顺便把「ls 的稳定输出来自排序而不是内核」这件事写在代码里。
func List(dir string) ([]fs.DirEntry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	entries, err := f.ReadDir(-1) // n = -1：一次读完，不分页
	if err != nil {
		return nil, fmt.Errorf("ReadDir %s: %w", dir, err)
	}
	slices.SortFunc(entries, func(a, b fs.DirEntry) int {
		return cmp.Compare(a.Name(), b.Name())
	})
	return entries, nil
}

// TypeRune 返回 ls -l 第一位那种类型字符：
//
//	d 目录、l 符号链接、p 命名管道、s socket、
//	b 块设备、c 字符设备、- 常规文件。
//
// 用的是 DirEntry.Type()——它只含「类型位」、不含权限位，
// 列目录时内核会直接给出类型，无需为每个条目再做一次 stat。
// 检查顺序与 finfo.FormatMode 保持一致：块设备同时不带
// ModeCharDevice，要先于字符设备判断。
func TypeRune(e fs.DirEntry) byte {
	switch m := e.Type(); {
	case m.IsDir():
		return 'd'
	case m&fs.ModeSymlink != 0:
		return 'l'
	case m&fs.ModeNamedPipe != 0:
		return 'p'
	case m&fs.ModeSocket != 0:
		return 's'
	case m&fs.ModeDevice != 0: // 块设备：有 ModeDevice 而无 ModeCharDevice
		return 'b'
	case m&fs.ModeCharDevice != 0:
		return 'c'
	default:
		return '-'
	}
}
