// Command du 是 du(1) 的教学版：遍历目录，把所有常规文件的 Size 求和，
// 输出表观大小（apparent size）与文件数。
//
//	go run ./cmd/du [-h] [-dedup] [目录 ...]    不给目录时统计当前目录 .
//
// 与真实 du 的差别要能说出来（详见 docs/04 的实验 2）：
//   - 统计的是文件逻辑长度之和，不是磁盘块占用（≈ du --apparent-size）；
//   - 默认硬链接会被重复计数（第 4 课口径）；加 -dedup 后按 (Dev,Ino)
//     去重，同一个 inode 只计一次（第 5 课补上课件，≈ 真实 du 默认行为）；
//   - 不跟随符号链接，目录表自身占的块也不计。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"example.com/gofs/internal/walkx"
)

func main() {
	log.SetFlags(0)
	human := flag.Bool("h", false, "人类可读单位（B/KiB/MiB/...）")
	dedup := flag.Bool("dedup", false, "按 (设备号,inode) 对硬链接去重，同一文件只计一次")
	flag.Parse()

	roots := flag.Args()
	if len(roots) == 0 {
		roots = []string{"."}
	}

	exitOK := true
	for _, root := range roots {
		bytes, files, err := walkx.DirSize(root, *dedup)
		if err != nil {
			// 部分结果仍然有效：先打印，再把错误报在 stderr。
			exitOK = false
			log.Printf("du: %s: %v", root, err)
		}
		tag := ""
		if *dedup {
			tag = "（硬链接去重）"
		}
		if *human {
			fmt.Printf("%-8s %6d 文件  %s%s\n", humanize(bytes), files, root, tag)
		} else {
			fmt.Printf("%d\t%d 文件\t%s%s\n", bytes, files, root, tag)
		}
	}
	if !exitOK {
		os.Exit(1)
	}
}

// humanize 按 1024 进制把字节数翻译成 12.3 KiB 这种形式；不足 1 KiB 保留 B。
func humanize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
