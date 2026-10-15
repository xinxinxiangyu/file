// Command find 是 find(1) 的教学版：用 WalkDir 遍历目录树，按条件
// 输出命中的路径（每行一个）。
//
//	go run ./cmd/find [路径] [-name PATTERN] [-type f|d|l]
//
// 例：
//
//	go run ./cmd/find . -name '*.md'
//	go run ./cmd/find . -type d
//
// 路径必须写在 flag 前面（标准库 flag 包遇到第一个非 flag 参数就停止
// 解析，所以这里手工把路径从参数里剥出来，剩下的再交给 flag 包）。
// 多个条件之间是 AND；-name 用 filepath.Match 的 glob 语法匹配 basename，
// -type f/d/l 分别表示常规文件/目录/符号链接。
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"strings"

	"example.com/gofs/internal/walkx"
)

func main() {
	log.SetFlags(0)

	// 剥出首个路径参数：find . -name '*.md' 这种写法里 "." 在 flag 前面。
	args := os.Args[1:]
	root := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		root = args[0]
		args = args[1:]
	}

	fset := flag.NewFlagSet("find", flag.ContinueOnError)
	name := fset.String("name", "", "按 basename 做 glob 匹配（如 *.md）")
	kind := fset.String("type", "", "按类型匹配：f 常规文件 / d 目录 / l 符号链接")
	if err := fset.Parse(args); err != nil {
		os.Exit(2)
	}

	var matchers []walkx.Matcher
	if *name != "" {
		m, err := walkx.MatchName(*name)
		if err != nil {
			log.Fatalf("find: %v", err)
		}
		matchers = append(matchers, m)
	}
	if *kind != "" {
		if len(*kind) != 1 {
			log.Fatalf("find: -type 只接受单个字符 f/d/l，得到 %q", *kind)
		}
		m, err := walkx.MatchType((*kind)[0])
		if err != nil {
			log.Fatalf("find: %v", err)
		}
		matchers = append(matchers, m)
	}

	// 无条件 = 匹配全部，和真实 find 行为一致。
	match := all
	if len(matchers) > 0 {
		match = and(matchers)
	}

	found, err := walkx.Find(root, match)
	for _, p := range found {
		fmt.Println(p)
	}
	if err != nil {
		// 权限不足等：命中的路径已经打印，错误报 stderr，非零退出。
		log.Printf("find: %v", err)
		os.Exit(1)
	}
}

// all 匹配每个条目。
func all(_ string, _ fs.DirEntry) (bool, error) { return true, nil }

// and 把多个 matcher 组合成 AND：任一返回 false 即 false；
// 任一返回错误（含 fs.SkipDir 剪枝）立即向上传递。
func and(ms []walkx.Matcher) walkx.Matcher {
	return func(path string, d fs.DirEntry) (bool, error) {
		for _, m := range ms {
			ok, err := m(path, d)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
}
