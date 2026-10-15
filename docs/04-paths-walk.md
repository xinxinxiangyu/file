# 第 4 课：路径处理与目录树遍历

> 配套代码：[internal/walkx](../internal/walkx/walkx.go)（Tree、DirSize、
> Find、MatchName/MatchType 与测试）、[cmd/tree](../cmd/tree/main.go)、
> [cmd/du](../cmd/du/main.go)、[cmd/find](../cmd/find/main.go) 三个命令。

## 这一课解决什么问题

第 3 课只能操作一层目录，这一课解决两件事：

1. **路径字符串怎么处理**：`a/./b/../c` 归一化、取文件名和扩展名、
   拼跨平台路径——归 `path/filepath` 管。它的大多数函数是**纯字符串运算**，
   不碰文件系统。铁律：**永远不要手工拼路径字符串**（`root + "/" + name`），
   分隔符、多余斜杠、`..` 都有现成函数替你正确处理。
2. **整棵树怎么遍历**：`filepath.WalkDir` 用「深度优先 + 回调」的模型把
   第 3 课的单层 ReadDir 自动递归成整棵树。学完后写出三个日常真用得上的
   工具：`tree`、`du`、`find`。

## 心智模型：路径是文本，遍历是一张张读目录表

第 3 课说过，路径解析是内核在一张张「名字 → inode」的目录表里逐级查表。
这一课要分清两件容易混的事：

- `filepath.Clean("a/./b//")` → `"a/b"`：**纯文本变换**，不查任何表，
  文件不存在也照样算。它只是把路径的「书写形式」归一化。
- `filepath.WalkDir` 走的是**真实的树**：每进一个目录都真的调 getdents
  把目录表读出来，条目不存在、权限不足都会变成真实错误。

而 WalkDir 做的事，本质就是你手写一个递归：ReadDir 当前目录 → 对每个
条目回调 → 条目是目录就递归进去。标准库替你处理了排序、错误递送和剪枝，
但模型没有任何魔法。

## 先跑起来

在 `/tmp` 下搭一棵小树：

```sh
mkdir -p /tmp/gofs-walk/proj/{docs/img,src/lib,tmp}
echo '# demo'       > /tmp/gofs-walk/proj/README.md
echo 'package main' > /tmp/gofs-walk/proj/src/main.go
echo 'package lib'  > /tmp/gofs-walk/proj/src/lib/util.go
echo '![pic]'       > /tmp/gofs-walk/proj/docs/img/pic.md
: > /tmp/gofs-walk/proj/docs/notes.txt   # 空文件
```

tree：

```sh
$ go run ./cmd/tree /tmp/gofs-walk/proj
/tmp/gofs-walk/proj
├── README.md
├── docs
│   ├── img
│   │   └── pic.md
│   └── notes.txt
├── src
│   ├── lib
│   │   └── util.go
│   └── main.go
└── tmp

5 个目录，5 个文件
```

du（默认输出表观字节数和文件数，`-h` 换人类可读单位）：

```sh
$ go run ./cmd/du /tmp/gofs-walk/proj
39	5 文件	/tmp/gofs-walk/proj
$ go run ./cmd/du -h /tmp/gofs-walk/proj
39 B    5 文件  /tmp/gofs-walk/proj
```

find，支持 `-name`（basename glob）和 `-type f|d|l`，多个条件是 AND：

```sh
$ go run ./cmd/find /tmp/gofs-walk/proj -name '*.go'
/tmp/gofs-walk/proj/src/lib/util.go
/tmp/gofs-walk/proj/src/main.go
$ go run ./cmd/find /tmp/gofs-walk/proj -type f -name '*.md'
/tmp/gofs-walk/proj/README.md
/tmp/gofs-walk/proj/docs/img/pic.md
```

注意 `*.go` 的输出顺序：`lib/util.go` 排在 `src/main.go` **前面**。
WalkDir 在每个目录里按名字排序、深度优先——`src` 下 `lib`（目录）排在
`main.go` 前面，于是先钻进 `lib`。真实 find 不排序（按文件系统原始顺序），
我们的词序输出是 WalkDir 给的确定性保证。

## 逐个 API 读

### 1. filepath：纯字符串路径函数

| 函数 | 作用 | 例子 |
| --- | --- | --- |
| `Join(elem...)` | 用系统分隔符拼接并自动 Clean | `Join("a", "b", "../c")` → `a/c` |
| `Clean(p)` | 折叠 `.`、`..`、重复分隔符 | `Clean("a/./b//")` → `a/b` |
| `Base/Dir/Ext` | 取最后一段 / 所在目录 / 扩展名 | `/a/b.md` → `b.md`、`/a`、`.md` |
| `Abs(p)` | 相对路径 + 当前工作目录 → 绝对路径 | **会查进程状态** |
| `Rel(base, target)` | 算从 base 到 target 的相对路径 | `Rel("a","a/b/c")` → `b/c` |
| `SplitList(p)` | 拆 `PATH` 这种列表 | `"/bin:/usr/bin"` → 两段 |
| `Glob(pattern)` | **单层目录**通配匹配 | `Glob("*.go")`，不跨 `/` |
| `VolumeName(p)` | 取盘符/卷名 | macOS/Linux 为 `""`，Windows 为 `C:` |

Clean 的手算规则：把路径当成一段段 token，`.` 丢弃，普通段入栈，
`..` 弹掉栈顶一个普通段（开头的 `..` 无法抵消，保留）。所以
`a/./b//` → 栈 `[a, b]` → `a/b`；`a/b/../../c` → `c`；
`../../a` 开头的 `..` 保留 → `../../a`。测试 `TestFilepathBasics`
把这组事实钉死了。

`Abs` 和 `Rel` 是表里少数**不完全是字符串运算**的：Abs 要读进程当前工作
目录（第 3 课的 Chdir 会影响它）；Rel 在无法用纯文本求出（比如 Windows
跨盘符）时返回错误。

### 2. Glob 只管一层，跨树才用 WalkDir

`filepath.Glob("src/*.go")` 只匹配 `src` 这一张目录表里的名字，`*` 不跨
分隔符；想找任意深度的 `*.go` 得用 WalkDir + MatchName，这正是 cmd/find
做的事。把 Glob 理解成「在一张目录表里按模式筛名字」，就不会拿它当 find 用。

### 3. WalkDir 的回调模型

```go
filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error)
```

四条要先记住的行为：

1. **深度优先、词序**：先回调父目录，再按名字升序递归每个子目录。
   回调里的 `path` 是**已经 Join 好的完整路径**，直接能用。
2. **回调先于下降**：目录自己先被回调一次，然后才轮到它的孩子。
   所以 du 统计文件数、find 匹配目录，都能在同一个回调里处理。
3. **给的是 DirEntry**：就是第 3 课那个轻量条目，自带名字和类型位；
   要大小得对具体条目 `d.Info()`（一次按需 stat），du 就是这么做的。
4. **返回值控制遍历**：返回 `nil` 继续；返回 `fs.SkipDir` 剪枝；
   返回其他错误立即中止，且该错误成为 WalkDir 的返回值。

Go 1.16 之前用的是 `filepath.Walk`（回调给 `os.FileInfo`，每个条目都
stat 一遍）；只读类型和名字的场景一律用 WalkDir。

### 4. 回调里的 err 参数：错误是「送进来」给你决定的

签名里的 `err` 是这一 API 最容易被忽略、也最体现设计的地方：
**某个子目录读失败时，WalkDir 不会自作主张中止，而是把错误作为 err 参数
送进回调，由你决定继续还是中止。** 三种典型情形：

- root 本身 Lstat 失败（路径不存在）：第一次回调就是 `d == nil, err != nil`，
  名字都没有；我们的处理是记下错误、return nil，让遍历自然结束。
- 某个子目录权限不足：父目录的 ReadDir 已经拿到了它的**名字和类型**，
  所以回调里 `d != nil, err != nil`——find 仍会把这个名字交给 matcher
  （真实 find 也会打印它），du 跳过这棵子树继续汇总其余部分。
- root 下其他分支不受影响。

walkx 的统一做法是**不丢错误**：收集到 `errs`，遍历结束用 `errors.Join`
连同部分结果一起返回。cmd 端先打印已有的部分结果，再把错误报在 stderr
并以状态码 1 退出：

```text
$ chmod 000 .../proj/secret
$ go run ./cmd/du /tmp/gofs-walk/proj
du: .../proj: open .../proj/secret: permission denied
39	5 文件	/tmp/gofs-walk/proj      # 兄弟文件照常统计
```

测试 `TestDirSizeUnreadableDir` 钉住这个行为（root 用户绕过权限，该用例自动
跳过）；直接 `return err` 让一个子目录的失败炸掉整棵树，是最常见的错误写法。

### 5. SkipDir：唯一正规的剪枝手段

回调对一个目录返回 `fs.SkipDir`，WalkDir 就跳过它的整棵子树；对普通文件
返回则跳过同目录剩下的条目。walkx 把剪枝权直接交给 Find 的 Matcher：

```go
if d.Name() == "sub" {
    return false, fs.SkipDir   // sub 自己和下面的一切都不进入结果
}
```

WalkDir **不直接告诉你深度**，需要 `-maxdepth` 这类控制时自己算：
`rel, _ := filepath.Rel(root, path); depth := strings.Count(rel, "/")`
（rel 是 `"."` 时深度为 0）。这也是 Tree 画缩进用的办法。

### 6. 符号链接不跟随

WalkDir 遇到指向目录的符号链接时，**链接本身作为一个条目回调一次，不会
递归进它指向的目录**。这是刻意的安全默认：跟随链接可能重复计数，目录链接
成环还会无限递归。测试 `TestFindDoesNotFollowSymlink` 验证 `sub-link → sub`
时 `nested.log` 只出现一次。真要跟随，得自己在回调里 `os.Stat`（注意 Stat
跟随链接、Lstat 不跟随，第 5 课细讲）并手动处理环。

### 7. 三个工具各自的关键点

- **Tree 的难点是连接线**：回调是流式的，画某个条目时还不知道它是不是
  父目录的最后一个孩子（决定 `└──` 还是 `├──`、祖先的竖线要不要延续）。
  walkx 的做法是两趟：WalkDir 先按访问顺序收集 `{name, depth}`，渲染前
  用一个「祖先栈」预处理出每个节点是否最后一个孩子——同一父目录的孩子
  在 DFS 序列里连续出现，后出现的兄弟把前者的「最后」头衔顶替掉。
- **DirSize 的口径**（和真实 du 的差异，面试和实验都用得上）：
  只统计常规文件的 `Size()` 之和 = **表观大小**（≈ `du --apparent-size`），
  不是磁盘块占用；目录表自身大小不计、符号链接不计、**硬链接重复计数**。
- **Find = 遍历 + 组合 Matcher**：`MatchName` 用 filepath.Match 匹配
  basename（`*` 不跨目录）；`MatchType('f'|'d'|'l')` 看类型位；cmd/find
  用 AND 把多个 matcher 串起来。非法 glob（如 `[`）在构造 matcher 时就
  报错——不能等遍历完整棵树才发现模式写错。

### 8. 预告：filepath.WalkDir vs fs.WalkDir

| | `filepath.WalkDir` | `fs.WalkDir(fsys, root, fn)` |
| --- | --- | --- |
| 遍历对象 | 真实文件系统 | 任意 `fs.FS`（磁盘目录、embed、HTTP、测试内存 FS） |
| 回调签名 | `(path string, d fs.DirEntry, err error)` | 几乎一样 |
| 剪枝 | `fs.SkipDir` | 相同 |

回调模型一模一样——第 7 课会看到同一份遍历逻辑可以跑在磁盘、嵌入文件
甚至 HTTP 上。这节课先用真实磁盘把模型学透。

## API → 系统调用 → 文件系统事实

| Go API | Unix 系统调用 | 背后的文件系统事实 |
| --- | --- | --- |
| `filepath.Join/Clean/Base/...` | 无（纯字符串运算） | 路径只是路径解析的「脚本文本」 |
| `filepath.Abs` | `getcwd(2)` | 相对路径的起点是进程当前工作目录 |
| `filepath.Glob` | `getdents(2)`（一层） | 在一张目录表里按模式筛名字 |
| `filepath.WalkDir` | 递归 `lstat(2)` + `getdents(2)` | 把目录表一张张读过去；Lstat 不跟随链接 |
| 回调 `d.Info()` | `fstatat(2)` | DirEntry 只有名字类型，大小要按需补 stat |
| 回调里的权限错误 | `openat(2)` 返回 EACCES | 名字在父目录表里，但目标目录的表读不开 |
| 返回 `fs.SkipDir` | 无（库内剪枝） | 不发任何系统调用，直接跳过子树 |

## 建议阅读顺序

filepath 函数表（在终端用 `go run` 小片段把每个都试一遍）→ Clean 的栈式
手算规则 → WalkDir 回调签名与四条行为 → err 参数的三种情形（对照
`TestDirSizeUnreadableDir`）→ SkipDir 与深度计算 → [walkx.go](../internal/walkx/walkx.go)
里三个工具的实现（Tree 的两趟渲染最值得细看）→ 三个 cmd 的薄封装；
对照 [walkx_test.go](../internal/walkx/walkx_test.go) 看每个行为如何在
`t.TempDir()` 沙盒里被断言。

## 自己做三个实验

1. 给 cmd/find 加 `-maxdepth N`：用 Rel 算出深度，超过深度时对目录
   返回 `fs.SkipDir`（对普通文件返回 SkipDir 会连兄弟一起跳，注意区别）。
2. 遍历 `/usr/bin` 或 `/System` 下的大目录，对比 naive du 与系统
   `du -k` 的数字：想一想**块大小**（稀疏文件、不足一块也要占一块）、
   **硬链接**（du 只计一次，我们计多次）、**压缩文件系统**各造成什么差异。
3. 构造一个「名字里带换行/空格」的文件（`touch $'a\n b'`），看 naive
   按行输出的工具会怎样错乱——真实 find 的 `-print0`、xargs 的 `-0`
   为什么是必备设计？

读完后试着回答：**du 里同一个文件被硬链接多次会重复计数——「同一个文件」
到底怎么判定？文件名和文件是什么关系？** 下一课用链接 API 和 inode 号揭晓。

## 验收标准

- `go test ./...` 通过。
- 三个工具在任意目录上行为与系统 tree/du/find 大体一致，权限不足时
  输出部分结果而不是崩溃。
- 能解释 WalkDir 回调里 err 参数的意义、`d == nil` 与 `d != nil` 两种
  错误的区别，以及 SkipDir 的用法。
- 能手算 Clean、Join 的结果，说清 WalkDir 为什么不跟随符号链接。
