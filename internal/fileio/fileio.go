// Package fileio 是第 1 课抽出的小工具包。
//
// 这一课最容易被忽略的事实是：io.Writer 的 Write 不保证一次写完 p——
// 它允许返回 n < len(p) 且 err == nil（短写）。WriteFull 把这种
// 底层接口包成「要么全写完、要么返回错误」的可靠写法。
package fileio

import (
	"errors"
	"io"
)

// errZeroWrite 用于防御返回 (0, nil) 的 Writer：它既没写数据也没报错，
// 直接循环会永远转下去。
var errZeroWrite = errors.New("fileio: Write 返回 (0, nil)，写入没有进展")

// WriteFull 反复调用 w.Write，直到 p 全部写完，返回总字节数。
//
// 常规磁盘文件上 Write 几乎总是一次写完，但 API 契约不保证这一点
// （管道、socket、/proc 等特殊文件尤其常见短写），所以写循环要按
// 「n 从 0 累加到 len(p)」来组织，而不是调用一次就当成功。
func WriteFull(w io.Writer, p []byte) (int, error) {
	var n int
	for n < len(p) {
		m, err := w.Write(p[n:])
		if m > 0 {
			n += m
		}
		if err != nil {
			return n, err
		}
		if m == 0 {
			return n, errZeroWrite
		}
	}
	return n, nil
}
