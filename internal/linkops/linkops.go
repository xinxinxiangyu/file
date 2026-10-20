// Package linkops 是第 5 课抽出的小工具包：创建硬链接与符号链接、
// 判定文件身份、识别悬空链接。
//
// 这一课的核心事实是文件系统的两层模型：
//
//   - 目录项（dentry）：一条「名字 → inode 号」的记录；
//   - inode：文件的真身，存着元数据（类型、权限、属主、大小、nlink……）
//     和数据块指针，本身不带名字。
//
// 硬链接（os.Link）只是往目录表里再加一条指向同一个 inode 的记录，
// 所以「两个名字是不是同一个文件」要比的不是路径字符串，而是
// FileInfo.Sys() 里的 (Dev, Ino) 身份；符号链接（os.Symlink）则是一个
// type=l 的独立小文件，内容是目标路径字符串——它认名字不认 inode。
package linkops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// Hard 给已存在的 src 再建一个名字 dst，二者指向同一个 inode。
//
// 对应 link(2)。三条系统强制的限制要记住：
//   - 不能给目录建硬链接（防止目录图出现环，Unix 一律 EPERM）；
//   - src 与 dst 必须在同一个文件系统上（inode 号只在同一设备内有意义，
//     跨设备返回 EXDEV/EPERM）；
//   - dst 已存在时报错（link 不覆盖）。
func Hard(src, dst string) error {
	if err := os.Link(src, dst); err != nil {
		return fmt.Errorf("硬链接 %s -> %s: %w", dst, src, err)
	}
	return nil
}

// Soft 创建符号链接 link，它的「内容」就是 target 这个路径字符串。
//
// 对应 symlink(2)。target 可以是相对路径（相对 link 所在目录解析，不是
// 相对进程 cwd）、可以指向目录、可以跨文件系统、甚至可以尚不存在——
// 创建时内核根本不去解析 target，所以悬空链接是合法的。
func Soft(target, link string) error {
	if err := os.Symlink(target, link); err != nil {
		return fmt.Errorf("符号链接 %s -> %s: %w", link, target, err)
	}
	return nil
}

// Identity 返回文件的身份：设备号 dev 与 inode 号 ino。
//
// 两个名字 (dev, ino) 完全相同，就是同一个文件——即使路径字符串天差地别、
// 即使相对/绝对写法不同。比 ino 时必须带上 dev：不同设备上的 inode 号
// 各自独立编号，可能撞号。os.SameFile 内部比的也是这一对。
//
// Go 跨平台的 FileInfo 不暴露 inode，这里取 Sys() 里平台相关的 *syscall.Stat_t
// （Unix 的 stat 结构），所以本包只支持 Unix（macOS/Linux）。
func Identity(info os.FileInfo) (dev, ino uint64) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		panic(fmt.Sprintf("linkops: 不支持的 FileInfo.Sys 类型 %T（本包仅支持 Unix）", info.Sys()))
	}
	return uint64(stat.Dev), uint64(stat.Ino)
}

// Nlink 返回 inode 当前的硬链接数，即有多少个名字指向它。
//
// 每建一个硬链接 nlink+1，每删一个名字（unlink/Remove）nlink-1；
// 数据块在 nlink 归零**且**没有进程仍打开该文件时才释放——所以
// 删掉一个有 3 个名字的文件，另外 2 个名字（以及打开它的进程）
// 照样能读到全部数据。
func Nlink(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		panic(fmt.Sprintf("linkops: 不支持的 FileInfo.Sys 类型 %T（本包仅支持 Unix）", info.Sys()))
	}
	return uint64(stat.Nlink)
}

// IsDangling 判断 path 是不是一个「悬空」的符号链接：链接本身在，
// 但顺着它找不到目标（Stat 返回 ErrNotExist）。
//
// 判定口径：
//   - path 不存在：返回 false 和错误（没有链接，谈不上悬空）；
//   - path 不是符号链接：返回 false, nil；
//   - 是符号链接且 Stat 跟随成功：false, nil；
//   - 是符号链接且 Stat 返回 fs.ErrNotExist：true, nil
//     （目标被删/改名，或目标路径从来不存在）；
//   - Stat 返回其他错误（ELOOP 链接成环、路径中权限不足等）：
//     无法判定，原样返回 false, err。
func IsDangling(path string) (bool, error) {
	li, err := os.Lstat(path) // Lstat：看链接本身，不能跟随
	if err != nil {
		return false, err
	}
	if li.Mode()&fs.ModeSymlink == 0 {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// SameFile 判断两个路径是否指向同一个文件（两边都用 Stat 跟随符号链接）。
//
// 内部就是各自 os.Stat 后交给 os.SameFile 比较 (Dev, Ino)；这里封成
// 路径版是为了课堂演示顺手。注意用的是 Stat 而非 Lstat：一个名字本身、
// 与另一个指向它的符号链接，SameFile 为 true——它们读写同一份数据。
func SameFile(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", a, err)
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", b, err)
	}
	return os.SameFile(ia, ib), nil
}
