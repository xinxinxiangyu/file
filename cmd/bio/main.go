// Command bio 是第 1 课的演示程序：在临时目录里打开真实文件，
// 依次演示写入、读回、Seek、部分读、EOF、ReadAt，以及
// O_APPEND / O_TRUNC 标志和「EOF 不是永久结束」。
//
// 运行：go run ./cmd/bio
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"example.com/gofs/internal/fileio"
)

func main() {
	log.SetFlags(0)

	dir, err := os.MkdirTemp("", "bio-")
	if err != nil {
		log.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(dir)
	fmt.Printf("临时目录: %s\n\n", dir)

	path := filepath.Join(dir, "segments.txt")

	// ---- 步骤 1：打开文件 -------------------------------------------------
	// O_RDWR    可读可写（与 O_RDONLY / O_WRONLY 三选一）
	// O_CREATE  不存在就创建；只有真的创建时，第三个参数 0o644 才会用上
	//           （且会被进程的 umask 屏蔽，第 2 课细讲）
	// O_TRUNC   文件已存在则把长度截成 0
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		log.Fatalf("OpenFile: %v", err)
	}
	fmt.Printf("已打开 %s（O_RDWR|O_CREATE|O_TRUNC, 0644）\n", path)

	// ---- 步骤 2：分 3 段写入 ---------------------------------------------
	// 注意第三段是中文：它占 9 个字节（UTF-8 一个汉字 3 字节），
	// 文件只认字节，不认「字符」。
	segs := [][]byte{
		[]byte("first-"),
		[]byte("second-"),
		[]byte("第三段"),
	}
	var total int
	for i, seg := range segs {
		n, err := fileio.WriteFull(f, seg)
		if err != nil {
			log.Fatalf("写入第 %d 段: %v", i+1, err)
		}
		total += n
		fmt.Printf("写入第 %d 段 %q：%d 字节（文件累计 %d 字节）\n", i+1, seg, n, total)
	}
	fmt.Println()

	// ---- 步骤 3：Seek 回开头，完整读回对比 --------------------------------
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		log.Fatalf("Seek 到开头: %v", err)
	}
	got := make([]byte, total)
	if _, err := io.ReadFull(f, got); err != nil {
		log.Fatalf("ReadFull: %v", err)
	}
	fmt.Printf("从头读回 %q，与写入内容一致：%v\n", got, bytes.Equal(got, bytes.Join(segs, nil)))

	// ---- 步骤 4：Seek 到中间一段读 ---------------------------------------
	if _, err := f.Seek(int64(len(segs[0])), io.SeekStart); err != nil {
		log.Fatalf("Seek 到第 2 段: %v", err)
	}
	mid := make([]byte, len(segs[1]))
	if _, err := io.ReadFull(f, mid); err != nil {
		log.Fatalf("读第 2 段: %v", err)
	}
	fmt.Printf("Seek(%d, Start) 后读到 %q\n", len(segs[0]), mid)

	// ---- 步骤 5：继续读到 EOF --------------------------------------------
	// ReadFull 读完第三段后，偏移正好在文件末尾。
	rest := make([]byte, len(segs[2]))
	if _, err := io.ReadFull(f, rest); err != nil {
		log.Fatalf("读第 3 段: %v", err)
	}
	fmt.Printf("继续读到 %q（%d 字节，不是 3 个「字符」）\n", rest, len(rest))
	n, err := f.Read(make([]byte, 1))
	fmt.Printf("偏移位于末尾时 Read 返回 n=%d, err=%v（这就是 io.EOF）\n\n", n, err)

	// ---- 步骤 6：ReadAt 按绝对偏移读，且不移动偏移 ------------------------
	at := make([]byte, len(segs[1]))
	if _, err := f.ReadAt(at, int64(len(segs[0]))); err != nil {
		log.Fatalf("ReadAt: %v", err)
	}
	_, err = f.Read(make([]byte, 1)) // 偏移仍在末尾
	fmt.Printf("ReadAt(%d) 读到 %q；紧接着的普通 Read 仍得到 %v —— ReadAt 不动共享偏移\n\n",
		len(segs[0]), at, err)

	// ---- 步骤 7：写场景下显式检查 Close 的错误 ----------------------------
	if err := f.Close(); err != nil {
		log.Fatalf("Close: %v", err)
	}
	_, err = f.Read(make([]byte, 1))
	fmt.Printf("Close 后再 Read：%v；errors.Is(err, os.ErrClosed) = %v\n\n", err, errors.Is(err, os.ErrClosed))

	// // ---- 步骤 8：O_APPEND 追加，旧内容保留 --------------------------------
	ap, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("O_APPEND 打开: %v", err)
	}
	if _, err := fileio.WriteFull(ap, []byte("<-tail")); err != nil {
		log.Fatalf("追加: %v", err)
	}
	if err := ap.Close(); err != nil {
		log.Fatalf("close append: %v", err)
	}
	all, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("ReadFile: %v", err)
	}
	fmt.Printf("O_APPEND 写入后完整内容：%q\n", all)

	// ---- 步骤 9：O_TRUNC 打开瞬间清空 -------------------------------------
	tp, err := os.OpenFile(path, os.O_RDWR|os.O_TRUNC, 0o644)
	if err != nil {
		log.Fatalf("O_TRUNC 打开: %v", err)
	}
	info, err := tp.Stat()
	if err != nil {
		log.Fatalf("Stat: %v", err)
	}
	fmt.Printf("O_TRUNC 打开后文件大小：%d 字节（打开动作本身就完成了清空）\n", info.Size())
	if err := tp.Close(); err != nil {
		log.Fatalf("close trunc: %v", err)
	}
	fmt.Println()

	// ---- 步骤 10：EOF 不是「永久结束」 ------------------------------------
	// A、B 是两次独立 open 得到的两个句柄，各有各的偏移。
	rpath := filepath.Join(dir, "refresh.txt")
	if err := os.WriteFile(rpath, []byte("one"), 0o644); err != nil {
		log.Fatalf("准备 refresh.txt: %v", err)
	}
	a, err := os.Open(rpath)
	if err != nil {
		log.Fatalf("打开句柄 A: %v", err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(a, buf); err != nil {
		log.Fatalf("A 读: %v", err)
	}
	_, err = a.Read(make([]byte, 1))
	fmt.Printf("句柄 A 读到 %q 后再读：%v\n", buf, err)

	b, err := os.OpenFile(rpath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("打开句柄 B: %v", err)
	}
	if _, err := fileio.WriteFull(b, []byte("-two")); err != nil {
		log.Fatalf("B 追加: %v", err)
	}
	if err := b.Close(); err != nil {
		log.Fatalf("close B: %v", err)
	}

	more := make([]byte, 4)
	n, err = a.Read(more) // A 没有重新打开，偏移还停在旧末尾
	if err != nil {
		log.Fatalf("A 再次读: %v", err)
	}
	fmt.Printf("B 追加后，A 不重新打开直接 Read：读到 %q（n=%d, err=%v）—— EOF 只是「现在没数据」\n",
		more[:n], n, err)
	if err := a.Close(); err != nil {
		log.Fatalf("close A: %v", err)
	}
}
