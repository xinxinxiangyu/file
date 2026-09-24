# 第 1 课：打开文件、读写字节

> 配套代码：[cmd/bio/main.go](../cmd/bio/main.go)（演示）、
> [internal/fileio](../internal/fileio/fileio.go)（小工具包与测试）。

## 这一课解决什么问题

一切文件操作都始于打开。这一课通过 `os.OpenFile` 及各种 flag 理解「打开一个
文件」到底意味着什么：返回的 `*os.File` 代表一个文件描述符，附带可移动的
读写偏移；`Read/Write` 是字节流接口，没有「消息边界」；读超当前文件大小
会得到 EOF。同时观察 `O_APPEND`、`O_TRUNC`、`O_CREATE` 等标志的实际效果。

对应到 net 项目：TCP echo 学的是字节流与收发，这一课学的是**另一种字节流**
——没有对端、没有连接关闭，取而代之的是一个可来回移动的偏移量和一个含义
微妙的 EOF。

## 心智模型：一次 OpenFile 在内核里发生了什么

`open` 不是「把文件内容取进程序」，而是在内核里建立一条访问通道：

```text
 进程
  ┌──────────────┐
  │ 文件描述符 3  │ ──指向──┐
  └──────────────┘         │
                           ▼
              内核「打开文件描述」(open file description)
              ┌──────────────────────────────┐
              │ 当前偏移 offset = 0          │
              │ 打开标志 O_RDWR / O_APPEND…  │
              └──────────────────────────────┘
                           │ 指向
                           ▼
                        inode（文件本体：数据块 + 元数据）
```

三个要点：

1. **文件描述符（fd）只是一个小整数**（Go 把它包在 `*os.File` 里），是进程
   句柄表的下标；真正记录偏移和标志的是内核里的「打开文件描述」。
2. **两次独立 open 得到两个独立的打开文件描述**，各自维护自己的偏移——这正是
   后面「A 句柄读到 EOF、B 句柄追加、A 继续读」实验成立的原因。
3. **open 时内核做了两件实事**：沿路径逐级查目录找到 inode（路径上每一级都要
   有可穿过的 x 权限，第 2 课讲），再检查访问权限；通过才发放入场券。

## 先跑起来

```sh
go run ./cmd/bio
```

演示全程在一个系统临时目录里操作真实文件，退出时自动清理。实际输出：

```text
临时目录: /var/folders/.../T/bio-478395275

已打开 .../segments.txt（O_RDWR|O_CREATE|O_TRUNC, 0644）
写入第 1 段 "first-"：6 字节（文件累计 6 字节）
写入第 2 段 "second-"：7 字节（文件累计 13 字节）
写入第 3 段 "第三段"：9 字节（文件累计 22 字节）

从头读回 "first-second-第三段"，与写入内容一致：true
Seek(6, Start) 后读到 "second-"
继续读到 "第三段"（9 字节，不是 3 个「字符」）
偏移位于末尾时 Read 返回 n=0, err=EOF（这就是 io.EOF）

ReadAt(6) 读到 "second-"；紧接着的普通 Read 仍得到 EOF —— ReadAt 不动共享偏移

Close 后再 Read：read .../segments.txt: file already closed；errors.Is(err, os.ErrClosed) = true

O_APPEND 写入后完整内容："first-second-第三段<-tail"
O_TRUNC 打开后文件大小：0 字节（打开动作本身就完成了清空）

句柄 A 读到 "one" 后再读：EOF
B 追加后，A 不重新打开直接 Read：读到 "-two"（n=4, err=<nil>）—— EOF 只是「现在没数据」
```

## 逐个 API 读

### 1. OpenFile 的参数

`os.OpenFile(name, flags, perm)`，对应系统调用 `open(2)`（macOS/Linux 现代
实现底层是 `openat(2)`）。

访问模式三选一：

| flag | 含义 |
| --- | --- |
| `O_RDONLY` | 只读（`os.Open` 就是它的快捷方式） |
| `O_WRONLY` | 只写 |
| `O_RDWR` | 可读可写 |

常用修饰标志，可以任意组合：

| flag | 效果 | 对应事实 |
| --- | --- | --- |
| `O_CREATE` | 不存在则创建；此时 `perm` 才生效 | 新建目录项指向新 inode |
| `O_TRUNC` | 已存在则打开瞬间截断成 0 字节 | open 动作里就完成，还没写就空了 |
| `O_APPEND` | 每次 Write 前强制把偏移拨到文件末尾 | 多进程写日志不会互相覆盖 |
| `O_EXCL` | 与 O_CREATE 同用：要求此前必须不存在 | 第 6 课用来做哨兵文件 |
| `O_SYNC` | 每次写同步落盘 | 第 6 课 fsync 再细究 |

第三个参数 `perm`（如 `0o644`）**只在 O_CREATE 真正创建文件时使用**，且会被
进程的 **umask** 屏蔽——比如 umask 022 时请求 0666 也只会得到 0644。命令行
执行 `umask` 可以看到当前值，权限模型第 2 课展开。

`os.Create(name)` 是最常见的快捷方式，等价于
`OpenFile(name, O_RDWR|O_CREATE|O_TRUNC, 0666)`。

### 2. Write 与「短写」循环

`f.Write(p)` 的契约是：返回实际写入的 `n`，**允许 `n < len(p)` 且 `err == nil`**
（短写）。常规磁盘文件上几乎总是一次写完，但管道、socket、特殊文件上不一定，
所以不能调用一次就当成功。

正确写法是按「已写入 n 从 0 累加到 len(p)」循环，这就是
[internal/fileio](../internal/fileio/fileio.go) 的 `WriteFull`：

```go
var n int
for n < len(p) {
    m, err := w.Write(p[n:])
    if m > 0 { n += m }
    if err != nil { return n, err }
    if m == 0  { return n, errZeroWrite } // (0,nil) 意味着永远不会进展
}
```

另一个细节：Write 从当前偏移开始写并推进偏移；写到偏移超过原大小时，文件
被扩展，中间没覆盖到的空洞按 0 填充。

### 3. Read、部分读与 io.ReadFull

Read 同样**可能返回少于请求的字节而不返回错误**。所以：

- 想「恰好填满一个缓冲区」，用 `io.ReadFull(f, buf)`——它内部循环直到填满；
  若在填满前遇到 EOF，返回 `io.ErrUnexpectedEOF`。
- 想「读到文件结束为止」，用 `io.ReadAll` 或 `os.ReadFile`（后者 = open +
  读完 + close 的便捷封装）。
- 自己写循环时，判断结束的条件是 `n == 0 && errors.Is(err, io.EOF)`。

文件里只有字节，没有「字符」：`第三段` 是 3 个汉字、9 个字节（UTF-8 每个
汉字 3 字节），Go 的 `len(string)` 数的也是字节。

### 4. Seek：偏移量可以来回移动

`f.Seek(offset, whence)` 对应 `lseek(2)`，它**只改内核打开文件描述里的偏移
数字，不接触磁盘数据**：

| whence | 偏移计算 |
| --- | --- |
| `io.SeekStart` | 相对于开头 |
| `io.SeekCurrent` | 相对于当前位置（offset 可负） |
| `io.SeekEnd` | 相对于文件末尾（如 `-2` 表示倒数 2 字节处） |

可以随意倒回去重读、或跳过一段——这是文件与 TCP 字节流最直观的区别：
TCP 收过的字节不能「倒回去再收一遍」。

### 5. EOF 的精确语义

偏移到达或超过文件当前大小时，Read 返回 `(0, io.EOF)`。注意是「**当前**
大小」：EOF 不是一个被永久标记的状态。演示的最后一个实验：

1. 句柄 A 读到 `one` 后再读，得到 `io.EOF`；
2. 句柄 B（另一次独立 open）以 `O_APPEND` 追加 `-two` 后关闭；
3. A **没有重新打开**，偏移仍停在旧末尾（3）；文件已变长到 7，于是 A 直接
   Read 就得到了 `-two`，错误为 nil。

因为两个句柄的偏移彼此独立，A 的偏移没有被 B 的写入改变；而旧 EOF 在文件
变长后就不再是 EOF。

和第 1 门 net 课的 TCP EOF 对比：

| | 文件 EOF | TCP EOF（对端 FIN） |
| --- | --- | --- |
| 含义 | 偏移 ≥ 当前文件大小 | 对端关闭了发送方向 |
| 之后还能有新数据吗 | 能，文件被追加后继续读即可 | 不能，这条连接的读方向已终结 |
| 可恢复吗 | 无需任何操作，自然恢复 | 只能重新建立连接 |
| 类比 | 「现在没货了」 | 「店铺永久关门了」 |

### 6. ReadAt / WriteAt：不碰共享偏移

`f.ReadAt(buf, off)` 对应 `pread(2)`，`f.WriteAt(p, off)` 对应 `pwrite(2)`：
按绝对偏移读写，**不修改、也不依赖句柄的共享偏移**。

演示中句柄偏移在 EOF 时调用 `ReadAt(buf, 6)` 读到中间片段，紧接着的普通
Read 仍然返回 EOF——偏移纹丝不动。因此多个 goroutine 可以在同一文件的
**不同位置**用 ReadAt/WriteAt 并发操作而互不干扰（同一位置并发写仍需要第 6
课的锁）。

### 7. Close：归还描述符，写场景必须检查错误

`f.Close()` 对应 `close(2)`：fd 被归还，之后所有操作报 `os.ErrClosed`
（用 `errors.Is` 判断，不要匹配字符串）。

写场景下不能只写一句 `defer f.Close()` 就不管它：某些文件系统（NFS、缓冲
设备）延迟回写的错误可能在 Close 时才暴露。演示里的写法是**显式关闭并检查
返回值**：

```go
if err := f.Close(); err != nil {
    log.Fatalf("Close: %v", err)
}
```

需要「出错提前返回」的函数里常用的折中：`defer f.Close()` 保底，正常路径
末尾再显式 Close 检查一次（重复 Close 只返回 `os.ErrClosed`，无副作用）。

## API → 系统调用 → 文件系统事实

| Go API | Unix 系统调用 | 背后的文件系统事实 |
| --- | --- | --- |
| `os.OpenFile` | `openat(2)` | 路径逐级解析、权限检查，新建打开文件描述与 fd |
| `(*os.File).Read` | `read(2)` | 从打开文件描述的偏移复制字节，并推进偏移 |
| `(*os.File).Write` | `write(2)` | 从偏移写入数据、推进偏移，必要时扩展文件 |
| `(*os.File).Seek` | `lseek(2)` | 只改内存里的偏移数字，不发生磁盘 I/O |
| `ReadAt/WriteAt` | `pread/pwrite(2)` | 带偏移参数的读写，不动共享偏移 |
| `(*os.File).Close` | `close(2)` | 归还 fd；写回的错误可能在此暴露 |
| `O_APPEND` | open 标志 | 打开文件描述被打上标记，内核在每次 write 前拨偏移到末尾 |
| `O_TRUNC` | open 标志 | open 处理中直接把 inode 的长度置 0 |

## 建议阅读顺序

[main.go](../cmd/bio/main.go) 的 OpenFile（逐个 flag 解释）→
[fileio.go](../internal/fileio/fileio.go) 的 WriteFull 短写循环 →
`io.ReadFull` → Seek 与 whence → Read 返回 io.EOF → ReadAt 不动偏移 →
显式检查 Close → O_APPEND / O_TRUNC 对比 → 双句柄 EOF 刷新实验；
对照 [fileio_test.go](../internal/fileio/fileio_test.go) 看每个行为是怎么在
`t.TempDir()` 里被断言的。

## 自己做三个实验

1. 用每次只读 1 字节的循环读一个包含中文的文件，数清楚字节数与字符数的
   差别（Go 的 len 是字节数，UTF-8 一个汉字 3 字节）。再把 `range` 遍历
   字符串的结果和字节偏移对照着看。
2. 两个句柄打开同一个文件：A 句柄读到 EOF 后，B 句柄追加一段，A 再读
   ——验证 EOF 不是「永久结束」；再试试 A 是用 O_APPEND 方式打开的会不会
   影响实验结果。
3. 故意用 perm `0o000` 创建文件（以普通用户），再尝试读写它，观察权限
   错误长什么样、`errors.Is(err, fs.ErrPermission)` 是否为真——为第 2 课的
   权限模型预热。

读完后试着回答：**我能读写字节了，但怎么知道一个文件有多大、是谁的、
能不能执行？这些信息存在哪、用哪个 API 取？** 下一课看 Stat 与权限。

## 验收标准

- `go test ./...` 通过。
- 能说清每个常用 flag（O_CREATE/O_TRUNC/O_APPEND/O_RDWR）的效果，以及
  `perm` 参数什么时候才会生效。
- 能解释偏移量存在哪里、Seek 做了什么、ReadAt 与普通 Read 的区别。
- 能写对短写循环，并解释为什么不能把一次 Write/Read 的返回当成完整结果。
- 能解释文件 EOF 与 net 课里 TCP 的 EOF 含义有何不同。
