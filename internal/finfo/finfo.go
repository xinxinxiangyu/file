// Package finfo 是第 2 课抽出的小工具包：把 os.Stat 取回的那份
// 「档案」（os.FileInfo）翻译成人能读的 ls -l 风格信息。
//
// 这一课最容易混淆的两件事：
//   - Mode 里混装了「类型位」和「权限位」，FormatMode 输出的第一位是
//     类型（-/d/l…），后九位才是 rwx；
//   - 目录的 rwx 与普通文件含义不同（列条目 / 增删名字 / 穿过访问）。
//
// 包内解释属主、链接数等字段用到 syscall.Stat_t，仅适用于类 Unix。
package finfo

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

// FormatMode 把 FileMode 转成 ls -l 风格的 10 字符权限串。
//
// 第一位是文件类型：- 常规文件、d 目录、l 符号链接、p 管道、
// s socket、b/c 块/字符设备；后九位依次是属主、属组、其他人的 rwx。
// setuid/setgid/sticky 分别把对应组的 x 显示为 s/s/t（无 x 时大写 S/T）。
func FormatMode(m fs.FileMode) string {
	buf := make([]byte, 10)

	// 第一位：类型。检查顺序与 ls 一致：目录优先，再看各 Mode* 位。
	switch {
	case m.IsDir():
		buf[0] = 'd'
	case m&fs.ModeSymlink != 0:
		buf[0] = 'l'
	case m&fs.ModeNamedPipe != 0:
		buf[0] = 'p'
	case m&fs.ModeSocket != 0:
		buf[0] = 's'
	case m&fs.ModeDevice != 0: // 块设备：有 ModeDevice 而无 ModeCharDevice
		buf[0] = 'b'
	case m&fs.ModeCharDevice != 0:
		buf[0] = 'c'
	case m&fs.ModeIrregular != 0:
		buf[0] = '?'
	default:
		buf[0] = '-'
	}

	// 后九位：把权限位 m.Perm() 的 9 个比特映射成 rwx。
	// 最高位（0400）对应串首的 r，所以从 1<<8 往下数。
	const rwx = "rwxrwxrwx"
	perm := m.Perm()
	for i := 0; i < 9; i++ {
		if perm&(1<<uint(8-i)) != 0 {
			buf[i+1] = rwx[i]
		} else {
			buf[i+1] = '-'
		}
	}

	// 特殊位改写对应的「x 位置」：setuid→属主、setgid→属组、sticky→其他人。
	if m&fs.ModeSetuid != 0 {
		if buf[3] == 'x' {
			buf[3] = 's'
		} else {
			buf[3] = 'S'
		}
	}
	if m&fs.ModeSetgid != 0 {
		if buf[6] == 'x' {
			buf[6] = 's'
		} else {
			buf[6] = 'S'
		}
	}
	if m&fs.ModeSticky != 0 {
		if buf[9] == 'x' {
			buf[9] = 't'
		} else {
			buf[9] = 'T'
		}
	}
	return string(buf)
}

// OctalMode 返回 Unix st_mode 风格的八进制权限：0644 → uint32(0644)、
// 带 setuid 的 0755 → uint32(04755)。
//
// 注意 Go 把 setuid/setgid/sticky 放在 fs.ModeSetuid 等「高位」里，
// m.Perm() 只给低 9 位 rwx，所以这里要把特殊位手动折叠回
// 04000/02000/01000，才能得到 chmod 使用的那个八进制数。
func OctalMode(m fs.FileMode) uint32 {
	p := uint32(m.Perm())
	if m&fs.ModeSetuid != 0 {
		p |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		p |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		p |= 0o1000
	}
	return p
}

// Describe 输出 ls -l 风格的一行：
//
//	-rw-r--r--   1 alice  staff     1234 Sep 23 14:39 notes.txt
//
// 依次是权限串、硬链接数、属主、属组、字节数、修改时间、名字。
// 用户名/组名查不到（如纯数字 UID、名字服务不可用）时退化为数字 ID。
func Describe(info os.FileInfo) string {
	nlink, uid, gid := statFields(info)
	return fmt.Sprintf("%s %3d %-8s %-8s %8d %s %s",
		FormatMode(info.Mode()),
		nlink,
		userName(uid),
		groupName(gid),
		info.Size(),
		info.ModTime().Format("Jan 2 15:04"),
		info.Name(),
	)
}

// statFields 从底层 syscall.Stat_t 取硬链接数与属主属组。
// FileInfo.Sys() 在非 Unix 平台或拿不到时，链接数按 1、属主属组按 0 处理。
func statFields(info os.FileInfo) (nlink, uid, gid uint64) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 1, 0, 0
	}
	return uint64(st.Nlink), uint64(st.Uid), uint64(st.Gid)
}

// userName 把数字 UID 翻成登录名；失败就直接显示数字。
func userName(uid uint64) string {
	if u, err := user.LookupId(strconv.FormatUint(uid, 10)); err == nil {
		return u.Username
	}
	return strconv.FormatUint(uid, 10)
}

// groupName 把数字 GID 翻成组名；失败就直接显示数字。
func groupName(gid uint64) string {
	if g, err := user.LookupGroupId(strconv.FormatUint(gid, 10)); err == nil {
		return g.Name
	}
	return strconv.FormatUint(gid, 10)
}
