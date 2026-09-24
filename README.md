# 用 Go 的文件 API 理解文件系统

目标：通过实际调用 Go 标准库的文件系统 API，理解文件系统本身是怎么回事。
不实现文件系统，而是**用它**：读写、查权限、建目录、遍历整棵目录树、
建硬链接和符号链接、做原子更新、加文件锁，最后用 `io/fs` 接口把本地目录、
嵌入文件和 HTTP 服务统一起来。每一课保留独立示例，方便回看，全程只用标准库。

学习节奏对照 `test/net` 项目：先运行看到现象，再读 API 背后的系统调用与
文件系统概念，然后抽成 `internal/` 下的小工具包并配上在临时目录里运行的测试。

| 阶段 | 练习 | 验收目标 |
| --- | --- | --- |
| 1（起点） | 文件读写 | 用各种 flag 打开文件，正确处理部分读写、偏移量与 EOF |
| 2 | 文件属性与权限 | Stat 拿到全部元数据；解释并修改 rwx 权限、属主、时间戳 |
| 3 | 目录操作 | 建目录、列条目、切换工作目录；说清 DirEntry 与 FileInfo 的区别 |
| 4 | 路径与遍历 | filepath 处理路径；WalkDir 遍历目录树，写出自己的 tree/du/find |
| 5 | 链接与文件身份 | 硬链接、符号链接；用 inode 号判断“两个名字是不是同一个文件”；原子 rename |
| 6 | 原子更新与并发 | 临时文件 + rename 原子写、O_EXCL、fsync、咨询文件锁 |
| 7（最后一课） | io/fs 接口与生态 | 用 fs.FS 统一访问磁盘目录与 embed 嵌入文件，WalkDir/Glob，HTTP 暴露 |

## 和 net 项目的对应关系

| net（网络） | 本项目（文件 API） |
| --- | --- |
| TCP echo：字节流与收发 | open/read/write：字节流、偏移量与 EOF |
| 长度前缀分帧（自己约定协议） | —（本课不造结构，改为学习文件系统**已有**的规则：权限、目录、链接） |
| PostgreSQL 启动握手（对端状态） | Stat：从文件系统取回一个文件的“状态报告” |
| 消息类型与一轮对话的结束 | 目录条目与目录树的遍历 |
| TLS、超时、取消（可靠性） | 原子 rename、fsync、文件锁（可靠性） |
| database/sql/driver 标准接口 | io/fs 标准接口：磁盘、embed、HTTP 统一抽象 |

网络课在学「怎样和对端约定协议」；这门课在学「操作系统已经为我们约定好的
文件系统模型是什么」——每调用一个 API，都对应一个可以观察的文件系统事实。

## 第 1 课：打开文件、读写字节

```sh
go run ./cmd/bio
```

在临时文件上演示写入、读回、Seek 跳转、部分读和 EOF，理解文件描述符、
偏移量和 `O_APPEND` / `O_TRUNC` / `O_CREATE` 等打开标志。
完整讲解与实验见 [docs/01-file-io.md](docs/01-file-io.md)。

## 第 2 课：文件属性与权限

```sh
go run ./cmd/finfo .                       # 像 ls -l 一样列出当前目录
go run ./cmd/finfo -stat /etc/hosts        # 打印完整元数据
```

理解 `FileInfo` 的每个字段、`-rw-r--r--` 三组权限位、属主/属组、
文件大小与时间戳；亲手 chmod 并观察权限被拒绝时的错误。
完整讲解见 [docs/02-stat-perm.md](docs/02-stat-perm.md)。

## 第 3 课：目录与条目

```sh
go run ./cmd/dirs demo
go run ./cmd/dirs -list demo
```

在沙盒目录里 MkdirAll、创建文件、`ReadDir` 列条目，切换工作目录，
理解「目录也是一种文件」以及 `DirEntry`（轻量）与 `FileInfo`（完整）的区别。
完整讲解见 [docs/03-dirs.md](docs/03-dirs.md)。

## 第 4 课：路径处理与目录树遍历

```sh
go run ./cmd/tree .            # 自己实现的 tree
go run ./cmd/du .              # 自己实现的 du：统计目录大小
go run ./cmd/find . -name '*.md'
```

掌握 `path/filepath` 的 Join/Clean/Base/Dir/Rel/Glob，以及 `filepath.WalkDir`
的遍历模型和回调错误处理。
完整讲解见 [docs/04-paths-walk.md](docs/04-paths-walk.md)。

## 第 5 课：硬链接、符号链接与文件身份

```sh
go run ./cmd/links demo
```

在沙盒里建硬链接与符号链接，用 `Stat`/`Lstat` 对比，打印 inode 号与链接数，
验证「删除原名后文件还在」「rename 是原子的」。从 API 行为理解
「文件（inode）与文件名是两回事」。
完整讲解见 [docs/05-links.md](docs/05-links.md)。

## 第 6 课：原子更新、fsync 与文件锁

```sh
go run ./cmd/atomic config.json    # 写入中途崩溃也不会留下半截文件
go run ./cmd/lock counter.dat      # 两个终端同时运行，用文件锁串行化计数
```

掌握「临时文件 + fsync + rename」原子替换、`O_CREATE|O_EXCL`、
`f.Sync()` 的落盘语义，以及 `syscall.FcntlFlock` 咨询锁。
完整讲解见 [docs/06-atomic-locks.md](docs/06-atomic-locks.md)。

## 第 7 课：io/fs 接口与生态

```sh
go run ./cmd/fsview            # 用同一套 fs.FS 代码访问磁盘目录和 embed 嵌入文件
go run ./cmd/fshttp            # http.FileServerFS 把目录通过 HTTP 暴露在 :8080
```

理解 `fs.FS`、`fs.File`、`fs.ReadDirFile`、`fs.ValidPath`，
比较 `os.DirFS` 与 `embed.FS`，并直接复用 `fs.WalkDir`、`fs.Glob`。
完整讲解见 [docs/07-iofs.md](docs/07-iofs.md)。

## 每课的固定节奏

和 net 项目一样，每一课都按这个节奏走：

1. **先运行** `cmd/` 下的示例，看到预期输出，建立直觉。
2. **读 docs**：重点看「API → 系统调用 → 文件系统概念」的对应关系和对比表。
3. **按文档给出的顺序读代码**：先看 `internal/` 的小工具包，再看 `cmd/` 的演示。
4. **跑测试**：`go test ./...`，测试在 `t.TempDir()` 沙盒目录里制造真实的
   文件和目录（而不是 mock），因为我们要学的就是真实文件系统的行为。
5. **自己做三个实验**，回答文档结尾引向下一课的问题。

## 参考资料

- 手册：`man 2 open read write lseek stat chmod mkdir link symlink rename fsync flock`。
- 《操作系统导论》（OSTEP）：文件与目录、文件系统实现几章（概念背景，不需要动手实现）。
- 《UNIX 环境高级编程》（APUE）第 3、4 章：文件 I/O 与文件属性的权威讲解。
- Go 标准库文档：`os`、`io`、`path/filepath`、`io/fs`、`embed`、`syscall`。
