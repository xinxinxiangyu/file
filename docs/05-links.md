# 第 5 课：硬链接、符号链接与文件身份

> 配套代码：[internal/linkops](../internal/linkops/linkops.go)（Hard/Soft/
> Identity/Nlink/IsDangling/SameFile 与测试）、
> [cmd/links](../cmd/links/main.go)（完整沙盒剧本）；
> 同时回头给第 4 课的 du 补上了 inode 去重：
> [walkx.DirSize](../internal/walkx/walkx.go) 新增 `dedup` 参数，
> [cmd/du](../cmd/du/main.go) 新增 `-dedup` 开关。

## 这一课解决什么问题

第 4 课的 du 有个明确的缺陷：同一个文件被硬链接两次，它就计两遍——因为
它只数「名字」。这一课通过链接 API 理解文件系统最本质的模型：

1. **文件（inode）和文件名是两层东西**：一个 inode 可以有多个名字。
   硬链接就是同一个 inode 的另一个名字；符号链接则是一个「内容为路径
   字符串」的独立小文件。
2. **文件身份怎么判定**：不是比路径字符串，而是比 `Stat_t` 里的
   `(Dev, Ino)`。这一课用它给 du 去重，把第 4 课的缺陷闭环。
3. **rename 为什么原子**：它只改一张目录表里的一条记录。这个性质是
   下一课「崩溃也完好的文件更新」的基石，本课先建立直觉。

## 心智模型：目录项 + inode 的两层结构

前几课一直在用「路径逐级查目录表」的模型，这一课把目录表里那一行看清楚：

```
       目录表（目录文件的数据块）            inode 表            数据块
  ┌───────────────────────────┐      ┌──────────────────┐
  │ 名字 "a.txt" │ inode 1001 │─────▶│ inode 1001       │────▶ "...字节..."
  │ 名字 "b.txt" │ inode 1001 │─────▶│ nlink=2 大小=..  │
  └───────────────────────────┘      └──────────────────┘
```

- **目录项（dentry）**：一条「名字 → inode 号」的记录，住在父目录的
  数据块里。创建文件、rename、删除，改的都是这张表。
- **inode**：文件的真身，编号唯一（在同一设备内），存类型、权限、属主、
  时间戳、大小、**nlink（名字计数）**、数据块指针——唯独不存名字。
- 打开一个路径 = 逐张目录表查到 inode 号，再用 inode 号访问文件。

所以「创建硬链接」做的事少得惊人：**只往目录表里加一行，指向已有
inode**，不复制任何数据；`nlink` 加 1。而「删除文件」`unlink` 做的事
同样少：**只删目录项、nlink 减 1**；只有 `nlink == 0` **且**没有进程
还打开着这个文件时，内核才回收 inode 和数据块。这解释了 `rm` 一个有
3 个硬链接的文件，另外两个名字为什么毫发无损。

## 先跑起来

```sh
go run ./cmd/links sandbox     # 目录默认就叫 sandbox，开始前清空、结束后删除
```

每一步都把 inode 号和 nlink 打印出来（实际数字每次运行不同，只看关系）：

```text
create   sandbox/a.txt            ino=31635713 nlink=1 mode=-rw-r--r--
hardlink sandbox/b.txt            ino=31635713 nlink=2   ← 同一个 inode、链接数 +1
         SameFile(a.txt, b.txt) = true
write b.txt → 读 a.txt 得到 "从 b.txt 重写\n"——内容联动，它们本就是同一个文件

symlink  sandbox/c.lnk -> a.txt
lstat    sandbox/c.lnk  mode=lrwxr-xr-x ino=31635716（链接自己的 inode，≠ 31635713）size=5
stat     sandbox/c.lnk  跟随链接 → ino=31635713（就是 a.txt）
readlink sandbox/c.lnk  → "a.txt"（链接文件里存的原始字符串）

remove a.txt：b.txt 仍在！ino=31635713 nlink=1，内容 "从 b.txt 重写\n"
         c.lnk 此刻 dangling=true——它存的是名字 "a.txt"，名字没了
rename b.txt -> d.txt：ino=31635713（还是同一个），内容不变
         rename 只是在目录表里把一条记录改 key，inode 和数据纹丝不动
...
给目录建硬链接：operation not permitted（内核拒绝——否则目录图会成环）
软链环 x->y->x：Lstat 正常，EvalSymlinks 报错：too many links
         WalkDir 遍历沙盒正常结束——它不跟随符号链接

dudemo/ 里 data.txt、h1.txt、h2.txt 是同一个 inode：
du 第 4 课口径：70 字节、4 文件（硬链接计了 3 遍）
du -dedup 口径：30 字节、2 文件（(Dev,Ino) 去重，软链不计）
```

## 逐个 API 读

### 1. os.Link：加一个名字（硬链接）

```go
os.Link(oldname, newname)   // link(2)
```

- 不复制数据、不复制 inode，只在目录表里新增一项指向同一 inode，nlink+1。
- `newname` 已存在时报错——**硬链接不覆盖**。
- 三条系统级限制（测试 `TestHardLinkToDirFails`、`TestHardLinkExistingDstFails`
  钉住了其中两条）：
  1. **不能给目录建硬链接**（EPERM）：否则 `..` 式的父指针与多条路径会让
     目录图成环，`..` 语义崩溃；`.` 和 `..` 是内核自己维护的仅有的目录硬链接。
  2. **不能跨文件系统**（EXDEV）：inode 号是每设备独立编号的，另一台设备
     上的 inode 1001 毫无意义。
  3. 名字不能已存在。

### 2. os.Symlink：造一个内容是路径的小文件（符号链接）

```go
os.Symlink(target, linkPath)   // symlink(2)
os.Readlink(linkPath)           // readlink(2)：取回链接里存的路径字符串
filepath.EvalSymlinks(path)     // 反复 readlink 直到真实文件
```

符号链接是一个**独立 inode**，`type=l`，它的文件内容就是 `target` 这串
字节（所以 Lstat 看到的大小正好等于路径字符串长度）。创建时内核**根本不
检查 target 是否存在**，因此它：

- 可指向文件、目录、不存在的路径（**悬空链接合法**）；
- 可跨文件系统（存的只是文本）；
- 相对路径相对**链接自己所在的目录**解析，不是相对进程 cwd
  （测试 `TestSymlinkAcrossDirectories`：`sub/up.lnk -> ../a.txt`）。

代价是路径解析要多走一跳：解析到 type=l 的 inode，读出字符串，再解析
一遍。链接环（`x→y→x`）会让内核在内置次数（Linux 40 跳）后放弃，
`EvalSymlinks` 返回 `too many links`（ELOOP）。

### 3. Stat 与 Lstat：一个跟随，一个不跟随

| | `os.Stat` | `os.Lstat` |
| --- | --- | --- |
| 作用在符号链接上 | **跟随**，返回目标的 FileInfo | 看**链接本身**，返回链接的 FileInfo |
| 对 `c.lnk → a.txt` 的类型 | `-`（常规文件） | `l`（符号链接） |
| 对 `c.lnk → a.txt` 的 inode | a.txt 的 inode | c.lnk 自己的 inode |
| 底层系统调用 | `stat(2)` | `lstat(2)` |
| 悬空链接 | 返回 ErrNotExist | **成功**（链接自己在） |

cmd/links 步骤 4 把这四行差异一次打全。列目录（getdents / WalkDir）
默认是 Lstat 语义：**不跟随符号链接**，所以指向目录的链接只作为一个
条目出现、目录环也不会让遍历死循环（第 4 课测试
`TestFindDoesNotFollowSymlink` 已验证）。

### 4. 文件身份：(Dev, Ino) 与 os.SameFile

Go 跨平台的 `os.FileInfo` 不暴露 inode 号，要从平台相关的
`info.Sys()` 里取 Unix 的 `*syscall.Stat_t`：

```go
stat := info.Sys().(*syscall.Stat_t)
stat.Dev     // 设备号：文件位于哪个文件系统
stat.Ino     // inode 号：该设备内的文件编号
stat.Nlink   // 硬链接数：这个 inode 有几个名字
```

**两个名字 `(Dev, Ino)` 完全相同，就是同一个文件。** 必须连 Dev 一起
比：inode 号只在同一设备内有意义，两块盘上各自的 inode 1001 互不相干。
标准库的 `os.SameFile(a, b FileInfo)` 内部比的也是这一对——
[linkops.SameFile](../internal/linkops/linkops.go) 是它的路径版
（两边用 `os.Stat`，所以「一个名字」和「指向它的符号链接」判为同一
文件：读写的是同一份数据）。

nlink 的生命周期，在测试里完整走了一遍：

- `TestHardLinkSameIdentity`：建链后两个名字看到的 nlink 都是 2
  （nlink 存在 inode 上，不分名字）；
- `TestRemoveOriginalKeepsHardLink`：删一个名字 nlink 回到 1、数据还在；
  删掉最后一个名字，Stat 才报 ErrNotExist；
- `TestHardLinkContentShared`：从一个名字写，另一个名字立刻读到——
  它们共用同一组数据块。

### 5. 悬空检测：Lstat 成功 + Stat 失败

「悬空」专属于符号链接：**链接自己在（Lstat 成功且类型为 l），但目标
解析不到（Stat 返回 ErrNotExist）**。两种典型成因：

- 目标从来不存在（软链创建时不检查目标）；
- 目标后来被删或改名——软链存的是**名字**，这是它和硬链接最尖锐的
  对比：`TestRenameTargetDanglesSymlink` 里 rename 目标后软链立刻悬空，
  而硬链接指向 inode，改名动不了它。

[linkops.IsDangling](../internal/linkops/linkops.go) 的口径要分清三种
否定：不是链接 → false；链接且 Stat 成功 → false；路径整体不存在 →
返回错误（没有链接，谈不上悬空）；ELOOP/权限不足等其他 Stat 错误也
原样上抛，不装成悬空。目标被补上后悬空链接立即「复活」，无需重建。

### 6. os.Rename：原子地改一条目录项

```go
os.Rename(oldpath, newpath)   // rename(2)
```

Rename 操作的是**目录表**：把一条目录项从一个位置移到另一个位置（同
目录改名）或从一张表移到另一张表（移动）。关键性质：

- **原子**：在同一文件系统内由内核一次操作完成，外部任何时刻看到的
  要么是旧名字、要么是新名字，不存在中间状态。所以「写临时文件再
  rename 盖上去」可以保证读者永远不会看到写了一半的文件（第 6 课展开）。
- **覆盖**：`newpath` 若是已存在的普通文件，会被原子地替换掉
  （`TestRenameAtomicOverwrite`）；但类型冲突（文件盖目录、目录盖文件）
  报错，且两边都保持原样（`TestRenameTypeConflictFails`）。
- **不动 inode**：rename 后 inode 号、nlink、数据全不变（cmd 步骤 6
  打印验证）。打开着该文件的进程甚至感觉不到。
- **跨文件系统失败**（EXDEV）：两台设备有两张独立的 inode 表，无法只
  改目录项完成；`mv` 遇到这种情况只能退化成「复制 + 删除」，也就不再
  原子。

注意被覆盖文件的数据释放仍遵守 nlink 规则：rename 只是换掉目录项指向。

### 7. 给 du 去重：把第 4 课的缺陷闭环

第 4 课的 `DirSize` 见名字就加大小，硬链接计两遍。修法是遍历时带一个
`map[[2]uint64]struct{}`，以 `(Dev, Ino)` 为 key：**同一个 inode 只在
第一次遇到时计大小，后面的名字跳过**。符号链接不跟随、不统计，所以
去重只影响真实常规文件。

```go
if dedup {
    stat := info.Sys().(*syscall.Stat_t)
    id := [2]uint64{uint64(stat.Dev), uint64(stat.Ino)}
    if _, dup := seen[id]; dup {
        return nil // 同一个 inode 的另一个名字
    }
    seen[id] = struct{}{}
}
```

两种口径都保留：默认不去重（让你看见缺陷），`go run ./cmd/du -dedup`
对应真实 du 的默认行为（du 也按 inode 去重；多个 root 各自独立计数，
我们的去重集合也在每次 `DirSize` 调用内新建）。测试
`TestDirSizeHardLinkDedup` 钉住：一个文件两个名字 + 一个无关文件，
不去重 12 字节/3 文件，去重后 7 字节/2 文件。

## 硬链接 vs 符号链接（至少能说出这些差别）

| | 硬链接 `os.Link` | 符号链接 `os.Symlink` |
| --- | --- | --- |
| 本质 | 同一 inode 的多一条目录项 | 独立 inode，内容是路径字符串 |
| inode 号 | 与目标**相同** | 与目标**不同**（自己是 type=l 的文件） |
| 指向 | inode（目标改名/删除不影响它） | 名字（目标改名/删除就**悬空**） |
| 目录 | 不能链接目录 | 可以 |
| 跨文件系统 | 不能（EXDEV） | 可以（只存文本） |
| 悬空 | 不可能（链接时目标必须在） | 合法且常见 |
| nlink | +1 / −1 | 不影响目标的 nlink |
| 解析开销 | 零跳（就是普通目录项） | 多一轮路径解析；成环报 ELOOP |
| 删除原文件 | 数据还在，直到最后一个名字消失 | 只剩悬空链接 |

记忆支点：**硬链接说「我们是平等的多个名字」，符号链接说「我只是一张
写着别人名字的便签」。**

## API → 系统调用 → 文件系统事实

| Go API | Unix 系统调用 | 背后的文件系统事实 |
| --- | --- | --- |
| `os.Link` | `link(2)` | 目录表加一行指向同一 inode，nlink+1，不碰数据 |
| `os.Symlink` | `symlink(2)` | 新建 type=l 的 inode，数据块里写路径字符串 |
| `os.Readlink` | `readlink(2)` | 原样读回那张「便签」上的字符串，不解析 |
| `filepath.EvalSymlinks` | 多次 `readlink(2)` | 一层层揭开便签直到真身；环 → ELOOP |
| `os.Stat` | `stat(2)` | 沿路径查表，type=l 时多解析一跳 |
| `os.Lstat` | `lstat(2)` | 沿路径查表但停在链接本身 |
| `info.Sys().(*syscall.Stat_t)` | stat 结构 | Dev/Ino 是文件身份，Nlink 是名字计数 |
| `os.Remove` | `unlink(2)` / `rmdir(2)` | 只删目录项、nlink−1；归零且无人打开才释放 |
| `os.Rename` | `rename(2)` | 原子地改/移一条目录项；同设备内成立，跨设备 EXDEV |
| `os.SameFile` | 无（库内比较） | 身份 = (Dev, Ino) 这一对数字 |

## 建议阅读顺序

目录项 → inode 两层模型（自己在纸上画 dentry 表和 inode 表）→
`(Dev, Ino)` 身份与 Nlink → unlink 的「延迟释放」语义 →
硬链接三条限制 → 符号链接与悬空、相对链接的解析基准 →
Stat/Lstat 对比表 → Rename 的原子性、覆盖与 EXDEV →
[linkops.go](../internal/linkops/linkops.go) 的薄封装 →
[cmd/links/main.go](../cmd/links/main.go) 的沙盒剧本（11 步正好对应
本文小节）→ walkx.DirSize 的去重补丁；最后对照
[linkops_test.go](../internal/linkops/linkops_test.go) 看每个行为
怎样在 `t.TempDir()` 沙盒里被断言。

## 自己做三个实验

1. 在 `/usr/bin`（macOS 上多为软链）或你自己的项目里统计：有多少 inode
   被多个名字共享。可以用本课的 `walkx.DirSize(root, false)` 与
   `(root, true)` 的差值来量化，再对比系统 `du` 与 `du -A`
   （`--apparent-size`）的数字。
2. 建一个符号链接环（`ln -s x y; ln -s y x`），分别试 ReadDir/WalkDir、
   `os.Stat`、`filepath.EvalSymlinks` 在它上面的行为：谁安全、谁报错、
   报什么错（ELOOP）。再建一条指向深层不存在路径的长链，数一跳
   readlink 的效果。
3. 验证 Rename 的原子覆盖：一个程序每秒读一个配置文件并解析，另一个
   程序反复用「写临时文件 + rename」更新它——读者永远不会读到解析失败
   的半截内容。再把更新方式换成直接 `>` 覆盖写，观察读者偶发的
   解析失败。为什么直接覆盖做不到？下一课回答。

读完后想问：**rename 为什么能原子？怎样用这个性质写出「崩溃也完好」的
文件更新？多个进程同时更新同一个文件又该怎么协调？** 下一课就是最后一个主题。

## 验收标准

- `go test ./...` 通过。
- 能说清硬链接与符号链接的 4 点以上差别（inode、目录、跨设备、悬空、
  nlink、解析开销……见对比表）。
- 会用 `(Dev, Ino)`（或 `os.SameFile`）判断两个名字是否同一文件，
  并已给 du 加上 `-dedup` 去重、对两种口径的差异有实测。
- 能解释为什么 rm 一个有 3 个硬链接的文件，数据还没消失（nlink 与
  「打开计数」两个条件都归零才释放）。
- 能解释软链悬空的两种成因，以及 Stat/Lstat/Readlink 各自在链接上的行为。
