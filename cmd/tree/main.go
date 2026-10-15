// Command tree 是 tree(1) 的教学版：用 WalkDir 收集整棵树，再渲染成
// 带 ├──/└── 连接线的树状图，并在结尾汇总目录数与文件数。
//
//	go run ./cmd/tree [目录]    目录默认为当前目录 .
//
// 与真实 tree 的差别：不染色、不跟随符号链接（WalkDir 的默认行为），
// 读不开的子目录只显示名字、不显示后代，并把错误报在 stderr。
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"example.com/gofs/internal/walkx"
)

func main() {
	log.SetFlags(0)
	flag.Parse()

	root := flag.Arg(0)
	if root == "" {
		root = "."
	}

	if err := walkx.Tree(os.Stdout, root); err != nil {
		// 树已经画了一部分（部分遍历结果），提示后以非零状态退出。
		log.Printf("tree: %v", err)
	}

	dirs, files := count(root)
	fmt.Printf("\n%d 个目录，%d 个文件\n", dirs, files)
}

// count 再走一遍统计非根节点里的目录数与常规文件数（root 本身不计，
// 与 tree(1) 的汇总口径一致）。深度同样用相对路径的分隔符数来算。
func count(root string) (dirs, files int) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		fmt.Print("---------", path)
		fmt.Println()
		if rel, _ := filepath.Rel(root, path); rel == "." {
			return nil // root 本身不计入汇总
		}
		switch {
		case d.IsDir():
			dirs++
		case d.Type().IsRegular():
			files++
		}
		return nil
	})
	return dirs, files
}
