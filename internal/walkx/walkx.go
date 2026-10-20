// Package walkx 是第 4 课抽出的小工具包：用 filepath.WalkDir 遍历整棵
// 目录树，实现三个日常工具的核心逻辑——tree（画树）、du（统计大小）、
// find（按条件找路径）。
//
// 这一课的核心事实分两层：
//
//   - path/filepath 处理的是「路径字符串」：Join/Clean/Rel 只做文本运算，
//     不碰文件系统；永远不要手工拼路径（漏分隔符、漏 Clean 是经典 bug）。
//   - filepath.WalkDir 处理的是「真实的树」：深度优先、词序访问，每个条目
//     把 名字（轻量 DirEntry）送进回调；读不开的目录以 err 参数告知，
//     返回 fs.SkipDir 是唯一正规的剪枝手段。
package walkx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Matcher 是 Find 的判定回调：对遍历到的每个条目调用一次。
//
// 返回 true 表示该路径计入结果；返回 fs.SkipDir 表示：
//   - 当前条目是目录：跳过整棵子树（剪枝）；
//   - 当前条目是普通文件：跳过同一目录里剩下的条目。
//
// 其他非 nil 错误会中止整次遍历。把剪枝权放进 Matcher，是为了让
// 「匹配什么」和「怎么遍历」解耦——-name、-maxdepth 之类的条件都能
// 组合在同一个回调里。
type Matcher func(path string, d fs.DirEntry) (bool, error)

// MatchName 返回一个按 basename 做 glob 匹配的 Matcher，等价 find -name。
//
// 用 filepath.Match 而不是正则：* 不跨目录、? 匹配单字符、[...] 字符集，
// 匹配的是 d.Name()（最后一段名字），不是完整路径。pattern 非法时在
// 遍历开始前就报错——不能等遍历完整棵树才发现模式写错了。
func MatchName(pattern string) (Matcher, error) {
	if _, err := filepath.Match(pattern, "probe"); err != nil {
		return nil, fmt.Errorf("name 模式 %q: %w", pattern, err)
	}
	return func(_ string, d fs.DirEntry) (bool, error) {
		return filepath.Match(pattern, d.Name())
	}, nil
}

// MatchType 返回按条目类型匹配的 Matcher，等价 find -type。
// 只支持三种常用类型：'f' 常规文件、'd' 目录、'l' 符号链接。
func MatchType(kind byte) (Matcher, error) {
	switch kind {
	case 'f':
		return func(_ string, d fs.DirEntry) (bool, error) {
			return d.Type().IsRegular(), nil
		}, nil
	case 'd':
		return func(_ string, d fs.DirEntry) (bool, error) {
			return d.IsDir(), nil
		}, nil
	case 'l':
		return func(_ string, d fs.DirEntry) (bool, error) {
			return d.Type()&fs.ModeSymlink != 0, nil
		}, nil
	default:
		return nil, fmt.Errorf("未知类型 %q：只支持 f（常规文件）、d（目录）、l（符号链接）", kind)
	}
}

// Find 遍历 root，返回所有 matcher 命中的路径（深度优先、词序，与
// 真实 find 的默认输出顺序一致）。
//
// 某个子目录读不开（权限不足等）时：条目的名字在父目录 ReadDir 时
// 已经拿到，所以仍然交给 matcher 判定，只是不会下降；底层错误被
// 收集起来，遍历结束后用 errors.Join 一并返回——结果可能是部分结果，
// 调用方自己决定丢弃还是展示。root 本身无法 Lstat 时结果为空。
func Find(root string, match Matcher) ([]string, error) {
	var found []string
	var errs []error

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			if d == nil {
				return nil // root 都访问不了，WalkDir 随即结束
			}
			// d 非 nil：名字已知，照常匹配，只是这棵子树无法下降。
		}
		ok, merr := match(path, d)
		if merr != nil {
			return merr // 含 fs.SkipDir：交给 WalkDir 解释成剪枝
		}
		if ok {
			found = append(found, path)
		}
		return nil
	})
	if walkErr != nil {
		return found, walkErr
	}
	return found, errors.Join(errs...)
}

// DirSize 遍历 root 求所有常规文件的表观大小（Size 之和）与文件数。
//
// 三个刻意的口径，和 du(1) 的差异都源自这里：
//   - 只统计 d.Type().IsRegular()：目录表自身的大小、符号链接都不计；
//   - 用的是 FileInfo.Size()（文件逻辑长度），不是磁盘块占用，
//     所以结果对应 du --apparent-size 而非默认 du；
//   - 硬链接默认仍会重复计数（第 4 课的原始行为）；dedup=true 时用
//     map[dev:ino] 对 inode 去重，同一个文件只计第一次——这是第 5 课
//     用「文件身份」补上的口径（对应真实 du 的默认行为）。
//
// 注意去重集合在一次 DirSize 调用内有效：多次调用（多个 root）各自独立，
// 与真实 du「每个 root 单独统计」一致。符号链接不跟随、不统计，所以
// 去重只针对真实的常规文件。
//
// 读不开的子目录不致命：跳过该分支、继续汇总其余部分，错误经
// 返回值带回（可能与部分结果同时出现）。
func DirSize(root string, dedup bool) (bytes int64, files int64, err error) {
	var errs []error
	seen := make(map[[2]uint64]struct{}) // (Dev, Ino) → 已计入

	walkErr := filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil // 进不去的目录：剪枝，继续遍历其余分支
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info() // DirEntry 不带大小，按需补一次 stat
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		if dedup {
			if id, ok := inodeIdentity(info); ok {
				if _, dup := seen[id]; dup {
					return nil // 同一个 inode 的另一个名字：只计第一次
				}
				seen[id] = struct{}{}
			}
		}
		bytes += info.Size()
		files++
		return nil
	})
	if walkErr != nil {
		return bytes, files, walkErr
	}
	return bytes, files, errors.Join(errs...)
}

// inodeIdentity 从 FileInfo.Sys() 里取 (Dev, Ino) 文件身份。
// Unix 上 Sys() 是 *syscall.Stat_t；其他平台断言失败时返回 false，
// 调用方退化为不去重（按名字计数）。
func inodeIdentity(info os.FileInfo) ([2]uint64, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return [2]uint64{}, false
	}
	return [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}, true
}

// treeNode 是 Tree 在遍历阶段记下的一个条目：只留画图需要的信息。
type treeNode struct {
	name  string
	depth int // root 为 0，root 的直接孩子为 1
}

// Tree 用 tree(1) 风格把 root 画到 w：
//
//	root
//	├── a.txt
//	└── sub
//	    ├── b.md
//	    └── deep
//
// WalkDir 的回调是「边走边看」的，画某个条目时无法知道它是不是父目录
// 的最后一个孩子（最后一个孩子决定用 └── 还是 ├──、竖线要不要延续），
// 所以这里分两趟：先 WalkDir 按访问顺序收集节点（顺带利用 WalkDir
// 保证的词序），再线性扫描渲染——判断某节点是否最后一个孩子，只需
// 看「下一个节点的深度是否不大于自己」。
//
// 读不开的目录照样画出它的名字（名字已知），只是没有后代；错误
// 收集后随返回值带回。
func Tree(w io.Writer, root string) error {
	var nodes []treeNode
	var errs []error

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			if d == nil {
				return nil
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err // 理论上不会发生：path 一定由 root Join 出来
		}
		depth := 0
		if rel != "." {
			// WalkDir 不直接给深度；用相对路径里的分隔符数算，
			// ToSlash 让这段逻辑在 Windows 上也成立。
			depth = strings.Count(filepath.ToSlash(rel), "/") + 1
		}
		nodes = append(nodes, treeNode{name: d.Name(), depth: depth})
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	// 预处理：标出每个节点是不是其父目录的最后一个孩子。
	// DFS 前序里同一父目录的孩子连续出现，所以维护「当前祖先栈」，
	// 按父节点记下最后出现的孩子即可——新来的兄弟会把前任顶替掉。
	// 不能只看下一个节点的深度：目录后面紧跟它自己的孩子（深度 +1），
	// 它是否最后一个孩子要等整棵子树走完才知道。
	isLast := make([]bool, len(nodes))
	lastChild := make(map[int]int) // 父节点 index → 当前最后一个孩子的 index
	stack := []int{0}              // stack[d] = 当前深度 d 的祖先节点 index（stack[0] 是 root）
	for i := 1; i < len(nodes); i++ {
		d := nodes[i].depth
		stack = stack[:d] // 弹掉已离开的深层目录，栈顶即父节点
		parent := stack[d-1]
		if prev, ok := lastChild[parent]; ok {
			isLast[prev] = false // 父目录又生了一个孩子，前任不再是最后一个
		}
		isLast[i] = true
		lastChild[parent] = i
		stack = append(stack, i)
	}

	// 渲染：头一行是 root 本身（Clean 去掉多余斜杠，与 tree(1) 一致）。
	fmt.Fprintln(w, filepath.Clean(root))

	// last[level-1] 记录该深度上「当前条目是否其父目录的最后一个孩子」，
	// 决定更深层条目前面的竖线该不该延续。
	last := make([]bool, 0, 8)
	for i := 1; i < len(nodes); i++ {
		n := nodes[i]

		for len(last) < n.depth {
			last = append(last, false)
		}
		last[n.depth-1] = isLast[i]

		var line strings.Builder
		for level := 1; level < n.depth; level++ {
			if last[level-1] {
				line.WriteString("    ") // 祖先已是最后一个孩子：竖线断开
			} else {
				line.WriteString("│   ")
			}
		}
		if isLast[i] {
			line.WriteString("└── ")
		} else {
			line.WriteString("├── ")
		}
		line.WriteString(n.name)
		fmt.Fprintln(w, line.String())
	}

	return errors.Join(errs...)
}
