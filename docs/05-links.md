# 第 5 课：硬链接、符号链接与文件身份

> 状态：计划稿（学到本课时再补完整讲解）

## 这一课实际解决什么问题

第 4 课的 du 会把硬链接重复计数，因为它只看「名字」。这一课通过链接 API
理解文件系统最本质的模型：**一个文件（inode）可以有多个名字**。硬链接就是
同一个 inode 的另一个名字；符号链接是一个「内容为路径」的独立小文件。
学会 Link/Symlink/Lstat/Readlink/Rename，并回头修正第 4 课的 du。

## 先跑起来

```sh
go run ./cmd/links sandbox
```

预期（关键证据都来自 Stat_t）：

```text
create   sandbox/a.txt       ino=12345 nlink=1 mode=-rw-r--r--
hardlink sandbox/b.txt       ino=12345 nlink=2   ← 同一个文件、链接数 +1
write b.txt                 → a.txt 的内容同步变（它们本来就是同一个文件）
symlink  sandbox/c.lnk -> a.txt
lstat c.lnk                 mode=l---------... ino=不同 ino，type=l
stat  c.lnk                 → 顺着读到 a.txt（ino=12345）
remove a.txt                → b.txt 仍在！数据只在 nlink 归零时释放
readlink c.lnk              → "a.txt"
rename b.txt -> d.txt        → c.lnk 指向的名字没了，变悬空链接
```

## 核心概念

1. **名字 ≠ 文件**：文件系统分两层——目录项（名字 → inode 号）和 inode
   （元数据 + 数据块）。创建硬链接只是加一条目录项指向同一 inode；所以：
   - 改任一个名字的内容，另一个看到同样变化；
   - `Stat_t.Ino`（+ Dev 设备号）是文件的身份；`(Dev, Ino)` 相同即同一文件；
   - `Nlink` 记录名字的数量；删除一个名字只是 nlink−1；**数据在 nlink 为 0
     且没有进程打开它时才真正释放**（打开的文件还在，第 6 课对锁有用）。
2. **硬链接限制**：不能给目录建硬链接（防止环）；不能跨文件系统（inode 号
   只在同一个设备内有意义）。
3. **符号链接**：`os.Symlink(target, link)`；它自己是个 type=l 的文件，
   内容就是目标路径字符串；可跨文件系统、可指向目录、可以悬空（目标不存在）。
4. **Stat/Lstat/Readlink**：Lstat 看链接本身（type=l、大小是路径字符串长度）；
   Stat 自动跟随；`os.Readlink` 取链接内容；`filepath.EvalSymlinks` 完整解析。
5. **Rename 原子且会覆盖**：`os.Rename` 在同一文件系统内由内核一步完成——
   任何时刻外部看到的要么是旧文件要么是新文件，不会有「半截」；目标位置
   若已有文件会被覆盖（但类型冲突如文件盖目录会报错）；跨设备则失败。
6. **修正 du**：遍历中用 `map[dev:inode]struct{}` 对硬链接去重，只计一次；
   这把第 4 课的已知缺陷闭环。

## 要写的代码

- `internal/linkops/linkops.go`：`Hard(src,dst)`、`Soft(target,link)`、
  `Identity(os.FileInfo) (dev,ino uint64)`、`IsDangling(path) (bool,error)`、
  `SameFile(a,b)`（可用现成 `os.SameFile`，包内给出实验验证）。
- `internal/linkops/linkops_test.go`：硬链接同 ino/nlink+1、内容联动、删原名
  不影响硬链接；软链 Lstat 类型 l、悬空检测、跨目录链接；SameFile 判定；
  Rename 原子覆盖；给目录建硬链接应失败。
- `cmd/links/main.go`：完整沙盒剧本；并给 du 打补丁做去重演示。

## 建议阅读顺序

目录项 → inode 两层模型 → `(Dev, Ino)` 身份 → Nlink 与删除语义 →
硬链接三条限制 → 符号链接与悬空 → Lstat/Stat 对比表 → Rename 原子性 →
du 去重闭环。

## 自己做三个实验

1. 在 `/usr/bin`（macOS 上多为软链）或你自己的项目里统计：有多少 inode
   被多个名字共享（用我们的代码遍历并计数），对比系统 du 的去重数字。
2. 建一个符号链接环（`ln -s x y; ln -s y x`），尝试 ReadDir/WalkDir 行为；
   再用 EvalSymlinks 看它怎么报错。
3. 验证 Rename 的原子覆盖：一个程序每秒读一个配置文件并解析，另一个程序
   反复用 rename 更新它——读者永远不会读到解析失败（半截内容）。
   为什么「直接 `>` 覆盖写」做不到？下一课回答。

读完后想问：**rename 为什么能原子？怎样用这个性质写出「崩溃也完好」的
文件更新？多个进程同时更新同一个文件又该怎么协调？** 下一课就是最后一个主题。

## 验收标准

- `go test ./...` 通过。
- 能说清硬链接与符号链接的 4 点以上差别（inode、目录、跨设备、悬空……）。
- 会用 (Dev, Ino) 判断两个名字是否同一文件，并已给 du 去重。
- 能解释为什么 rm 一个有 3 个硬链接的文件，数据还没消失。
