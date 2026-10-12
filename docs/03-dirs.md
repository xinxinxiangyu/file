# 第 3 课：目录与条目

> 配套代码：[cmd/dirs/main.go](../cmd/dirs/main.go)（建/列/Chdir/删全流程演示）、
> [internal/dirtree](../internal/dirtree/dirtree.go)（BuildLayout、排序 List、
> TypeRune 与测试）。

## 这一课解决什么问题

第 2 课把目录当成一个有特殊权限含义的对象，这一课直接操作它：创建、列条目、
删除，切换工作目录。通过 API 行为理解文件系统的核心模型——**目录是名字到
文件（inode）的映射表**；路径就是在一张张映射表里逐级查出来的。同时弄清
`DirEntry`（列目录时直接拿到的轻量信息）和 `FileInfo`（Stat 回来的完整档案）
的取舍。

## 心智模型：目录是一张映射表

第 2 课画过「目录项 → inode」的结构。把视角放大到路径解析：要找到
`/home/alice/docs/a.md`，内核做的事是

```text
/         → 查根目录这张表，找到名字 home    → inode 101
inode 101 → 查 home 这张表，找到名字 alice  → inode 202
inode 202 → 查 alice 这张表，找到名字 docs  → inode 303
inode 303 → 查 docs 这张表，找到名字 a.md   → inode 404
```

每一级目录就是一张「名字 → inode」的表；所谓路径，就是一连串查表操作的脚本
（沿途每一级都要有 x 才能穿过去——第 2 课的内容）。这一课的所有 API 都是在
操作这张表本身：

- `Mkdir` = 在父目录的表里**加一个名字**，并分配一个新 inode；
- `Remove` = 从表里**划掉一个名字**（inode 未必销毁，第 5 课讲链接数）；
- `ReadDir` = **把整张表读出来**。

目录本身也是一个 inode、也占数据块——它的「内容」就是这张表。这解释了
`FileInfo.Size()` 对目录取值的含义：演示里 `img` 目录显示 64 字节，那是这张
表在磁盘上占的大小，不是目录里所有文件的总大小。

## 先跑起来

```sh
go run ./cmd/dirs /tmp/gofs-demo
```

实际输出（macOS）：

```text
已在 /tmp/gofs-demo 搭好：docs/、docs/img/、docs/readme.md、tmp/

ReadDir(root)：
  d docs
  d tmp
ReadDir(docs)：
  d img
  - readme.md

列目录时只知道 readme.md 的名字和类型（-）；调 Info() 才拿到 大小 86 字节、权限 -rw-r--r--——一次额外的 stat。

列目录的三种接口：
  ReadDir      → 2 个 DirEntry：[readme.md img]（名字 + 类型，首选）
  Readdirnames → 2 个字符串：[readme.md img]（只要名字，最快）
  Readdir      → 2 个 FileInfo（老接口，每条带 Size/权限，可能逐个 stat）
                 readme.md  86 字节
                 img  64 字节

Chdir 前工作目录：/Users/zhangzc1.vendor/test/file
Chdir 后工作目录：/private/tmp/gofs-demo/tmp
WriteFile("chdir.txt") 实际落在 /tmp/gofs-demo/tmp/chdir.txt
已 Chdir 回 /Users/zhangzc1.vendor/test/file——并发程序里应直接用绝对路径，别依赖 Chdir。

Remove(chdir.txt)：常规文件直接删。
Remove(docs)（非空目录）：remove /tmp/gofs-demo/docs: directory not empty
    —— 非空目录只能 RemoveAll；tmp 已空，Remove 可以：
Remove(tmp)：空目录删除成功。

RemoveAll(root) 后 Stat：stat /tmp/gofs-demo: no such file or directory
errors.Is(err, fs.ErrNotExist) = true——沙盒已清空，演示结束。
```

也可以只列不建：

```sh
go run ./cmd/dirs -list /etc
```

```text
/etc 共 74 个条目（按名字排序）：
  - afpovertcp.cfg
  l aliases
  - aliases.db
  d apache2
  …
```

## 逐个 API 读

### 1. 创建：Mkdir 与 MkdirAll

| API | 行为 | 已存在时 |
| --- | --- | --- |
| `os.Mkdir(name, perm)` | 只建**一级** | 报错（EEXIST） |
| `os.MkdirAll(path, perm)` | 像 `mkdir -p`，沿途缺失的层级全建上 | **不报错** |

`Mkdir("a/b")` 而 `a/` 不存在时，错误是 ENOENT（中间级查不到）；这正是测试
`TestMkdirOnlyOneLevel` 钉住的行为。`BuildLayout` 全部走 MkdirAll，所以对同一棵
树重复执行不报错——幂等是搭沙盒、启动脚本最需要的性质。

第二个参数 `perm` 同样受 umask 影响（第 2 课），请求 0755、umask 022 → 实际 0755。

### 2. 列目录的三种方式

列目录要先 `os.Open` 打开目录，再对返回的 `*os.File` 调用（这就是目录 r 位
控制的操作）：

| 方法 | 返回 | 代价 |
| --- | --- | --- |
| `f.ReadDir(n)` | `[]fs.DirEntry` | 读表一次搞定，条目自带名字与类型；Go 1.16+ 首选 |
| `f.Readdirnames(n)` | `[]string` | 只要名字，最轻量 |
| `f.Readdir(n)` | `[]os.FileInfo` | 老接口，每一条可能触发额外 stat |

`n` 的含义三者一致：**`n = -1` 一次读完；小的正数用于分页**。`ReadDir(2)` 每次
最多返回 2 条，目录读到尽头时下一次调用返回空切片 + `io.EOF`。测试
`TestReadDirPaging` 验证了逐页拼接的结果与一次读完一致。处理超大目录时分页能
控制内存；普通目录直接 `-1` 最省心。

两个容易踩的细节：

1. **目录句柄有读取位置**，和普通文件的偏移量一样。用同一个 `f` 读完一遍后
   再调一次什么也得不到；想换读法就重新 `os.Open`（cmd 里每次都新开）。
2. **包级函数 `os.ReadDir(name)` 会替你按名字排序**；而 `f.ReadDir` 返回文件系统
   的原始顺序。`dirtree.List` 刻意用后者再自己排一遍，把排序这一步显式留在
   代码里。

### 3. DirEntry vs FileInfo

| | DirEntry（ReadDir） | FileInfo（Stat） |
| --- | --- | --- |
| 取名字 | `Name()` | `Name()` |
| 类型 | `Type()` / `IsDir()`：直接知道 | `Mode()` 含**完整权限** |
| 大小/时间 | 没有 | `Size()`、`ModTime()` |
| 代价 | 列目录一次系统调用基本搞定 | 每个文件单独一次 stat |
| 怎么补全 | 需要时 `entry.Info()` 按需 stat | 一开始就需要全部元数据时 |

`DirEntry.Type()` 返回的 `FileMode` **只装类型位、不含权限位**——这正是它轻量
的原因：类型是目录表记录里自带的。`TypeRune` 把这几个类型位翻译成
`ls -l` 第一位的 `d/l/p/s/b/c/-`，检查顺序与第 2 课的 `finfo.FormatMode` 一致。

经验法则：**列目录做筛选、统计目录数、打印名字，用 DirEntry 就够了；只有真的
要展示大小、权限、时间戳时，才对具体条目调 `Info()`。** 在大目录上，
`Readdir`（逐条 stat）和 `ReadDir` 的差距会非常明显。

### 4. 顺序不保证：排序是工具的事

内核不保证条目顺序，许多文件系统（ext4 的 htree、APFS 等）按哈希或块顺序
返回，新建文件也不一定出现在末尾。要稳定输出，就得像 `dirtree.List` 那样
拿到后用 `slices.SortFunc` 按名字排序——`ls`、`os.ReadDir` 都是这么做的。
任何测试断言也应先排序，否则就是在向文件系统的实现细节赌运气。

### 5. 工作目录：Getwd / Chdir

| API | 系统调用 | 含义 |
| --- | --- | --- |
| `os.Getwd()` | `getcwd(2)` | 取回进程当前工作目录的绝对路径 |
| `os.Chdir(dir)` | `chdir(2)` | 切换工作目录 |

进程解析任何**相对路径**，都以当前工作目录为起点。演示里 Chdir 到
`tmp/` 后写 `"chdir.txt"`，文件就落在 `tmp/` 里；Chdir 回原目录后，同一个相对
路径又指向别处。

两个必须记住的事实：

1. **Chdir 是整个进程的全局状态，影响所有 goroutine**。一个 goroutine Chdir，
   其他 goroutine 手里的相对路径语义瞬间改变——并发程序应直接用
   `filepath.Join(absRoot, name)` 算绝对路径，而不是 Chdir。
2. **Getwd 回来的是解析过符号链接的真实路径**。macOS 的 `/tmp` 是指向
   `/private/tmp` 的符号链接，所以演示里 Chdir 后 Getwd 显示
   `/private/tmp/gofs-demo/tmp`，与输入的 `/tmp/...` 文本不同但指同一个地方。

### 6. 删除：Remove 与 RemoveAll

| API | 系统调用 | 能删什么 |
| --- | --- | --- |
| `os.Remove(name)` | `unlink(2)`（文件）/ `rmdir(2)`（目录） | 常规文件、符号链接，或**空目录** |
| `os.RemoveAll(path)` | 递归 unlink + rmdir | 任何东西；路径**不存在也返回 nil** |

`Remove` 一个非空目录得到 ENOTEMPTY（`directory not empty`）——标准库没有为它
定义可移植的哨兵错误，测试里只断言非 nil。这与第 2 课呼应：删名字需要的是
**父目录的 w**，但 rmdir 额外要求「表已清空」，因为还有名字指向里面的 inode，
删掉目录会让那些名字变成悬空记录。

`RemoveAll` 对不存在的路径也返回 nil，所以清理代码可以无条件调用，幂等。

### 7. `.` / `..` 与根目录

每个目录都隐含 `.`（自己）和 `..`（上级）两个条目，根目录的 `..` 仍指向根自己。
但 **Go 的 ReadDir/Readdirnames 不会列出这两个名字**——它们由内核在路径解析时
特殊处理（`a/./b/../c` 的归一化下一课细讲），不属于目录表里的普通记录。

## API → 系统调用 → 文件系统事实

| Go API | Unix 系统调用 | 背后的文件系统事实 |
| --- | --- | --- |
| `os.Mkdir` | `mkdir(2)` | 在父目录的表里加名字，分配新 inode |
| `os.MkdirAll` | 多次 `mkdir(2)` | 逐级补建；已存在的 EEXIST 被忽略 |
| `f.ReadDir` | `getdents(2)` / `__getdirentries` | 读出目录表；记录自带名字与类型 |
| `f.Readdirnames` | 同上，只取名字字段 | 不做任何 stat |
| `f.Readdir` | getdents + 逐个 `stat(2)` | 老接口，为每条补完整档案 |
| `entry.Info()` | `fstatat(2)` | 对单个条目按需补 stat |
| `os.Getwd` | `getcwd(2)` | 内核逐级反查名字拼出绝对路径 |
| `os.Chdir` | `chdir(2)` | 改进程级的相对路径起点 |
| `os.Remove` | `unlink(2)` / `rmdir(2)` | 从表里划名字；目录必须为空 |
| `os.RemoveAll` | 递归 unlink/rmdir | 先清空表再删目录 |

## 建议阅读顺序

Mkdir/MkdirAll（加名字）→ [dirtree.go](../internal/dirtree/dirtree.go) 的
BuildLayout → os.Open 目录 + 三种读法对比 → DirEntry/FileInfo 对比表 →
`entry.Info()` 的按需 stat → 排序问题 → Getwd/Chdir（进程全局状态）→
Remove/RemoveAll（空目录规则）；
对照 [dirtree_test.go](../internal/dirtree/dirtree_test.go) 看每个行为如何在
`t.TempDir()` 沙盒里被断言。

## 自己做三个实验

1. 建一个含上千个文件的目录，对比 ReadDir（DirEntry）与老 Readdir
   （FileInfo）的耗时和系统调用次数（macOS 可用 `dtruss` 或用 time 粗测）。
2. ReadDir(2) 分页读大目录，验证拼接结果与一次读完一致；故意不读完就
   Close，理解 n 参数的用途。
3. 在 goroutine 里 Chdir，主 goroutine 同时用相对路径写文件，复现
   「Chdir 是进程全局状态」的坑；改用 `filepath.Join(absRoot, name)` 解决。

读完后试着回答：**现在能列一层目录了，怎么遍历整棵树？怎么把 `a/./b/../c`
这种路径归一化、怎么判断两个相对路径谁大谁小？** 下一课学习路径处理与 WalkDir。

## 验收标准

- `go test ./...` 通过。
- 能说清什么时候用 DirEntry、什么时候必须 Stat。
- 能解释为什么 Remove 一个非空目录会失败、MkdirAll 为什么不报错。
- 能说出 Chdir 在并发程序里的危险。
