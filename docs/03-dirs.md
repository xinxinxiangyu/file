# 第 3 课：目录与条目

> 状态：计划稿（学到本课时再补完整讲解）

## 这一课解决什么问题

第 2 课把目录当成一个有特殊权限含义的对象，这一课直接操作它：创建、列条目、
删除，切换工作目录。通过 API 行为理解文件系统的核心模型——**目录是名字到
文件的映射表**；路径就是在一张张映射表里逐级查出来的。同时弄清 `DirEntry`
（列目录时直接拿到的轻量信息）和 `FileInfo`（Stat 回来的完整档案）的取舍。

## 先跑起来

```sh
go run ./cmd/dirs sandbox
```

预期：在 `sandbox` 里搭出 `docs/`、`docs/readme.md`、`docs/img/`、`tmp/`；
然后 ReadDir 逐行打印类型字符与名字（与第 2 课的权限串衔接）；再演示
Getwd/Chdir 后写文件落在新的工作目录；最后 Remove/RemoveAll 清理。

也可以只列不建：

```sh
go run ./cmd/dirs -list /etc
```

## 核心概念

1. **创建与删除**：`os.Mkdir(name, perm)` 只建一级（已存在或路径中间缺失
   都会错）；`os.MkdirAll(path, perm)` 像 `mkdir -p`，已存在不报错；
   `os.Remove` 只能删空目录或空的常规文件；`os.RemoveAll` 递归删除。
2. **列目录的三种方式**（对 `*os.File` 调用，得用 os.Open 打开目录）：
   - `ReadDir(n)` 返回 `[]fs.DirEntry`：Go 1.16+ 首选，目录条目自带名字、
     是否目录，不需要对每个名字再 stat。
   - `Readdirnames(n)`：只要名字，最快。
   - `Readdir(n)`：老接口，返回 `[]FileInfo`，每一条可能触发额外 stat。
   `n = -1` 表示一次读完；小的正数用于分页处理超大目录。
3. **DirEntry vs FileInfo**：

   | | DirEntry（ReadDir） | FileInfo（Stat） |
   | --- | --- | --- |
   | 取名字 | Name() | Name() |
   | 方向 | 直接知道 IsDir/Type | Mode() 含完整权限、Size、ModTime |
   | 代价 | 列目录一次系统调用基本搞定 | 每个文件单独一次 stat |
   | 何时 Stat | 需要大小/权限时再 `entry.Info()` | 一开始就需要全部元数据时 |

4. **工作目录**：`os.Getwd`、`os.Chdir`（对应 getcwd/chdir）；进程的每个
   相对路径都以当前工作目录为起点。Chdir 影响**整个进程**（所有 goroutine），
   并发程序里尽量用绝对路径而不是 Chdir。
5. **目录条目不保证顺序**：许多文件系统返回哈希/目录顺序；要稳定输出就得
   `slices.SortFunc` 按名字排序（`ls` 也这么干）。
6. **根目录与 `.`/`..`**：每个目录都隐含 `.`（自己）和 `..`（上级），但
   Go 的 ReadDir 不会把这两个特殊条目列出来——它们在路径解析时由内核处理。

## 要写的代码

- `internal/dirtree/dirtree.go`：`BuildLayout(root string) error`（搭沙盒树）、
  `List(dir string) ([]fs.DirEntry, error)`（读目录并按名排序）、`TypeRune`。
- `internal/dirtree/dirtree_test.go`：在 `t.TempDir()` 里验证 MkdirAll、
  ReadDir 内容与顺序、Readdirnames、Remove 非空目录报错、RemoveAll 干净。
- `cmd/dirs/main.go`：build / -list 两种模式，含 Chdir/Getwd 演示。

## 建议阅读顺序

Mkdir/MkdirAll → os.Open 目录 + ReadDir → DirEntry/FileInfo 对比表 →
entry.Info() 的按需 stat → Getwd/Chdir → Remove 规则 → 排序问题。

## 自己做三个实验

1. 建一个含上千个文件的目录，对比 ReadDir（DirEntry）与老 Readdir
   （FileInfo）的耗时和系统调用次数（macOS 可用 `dtruss` 或用 time 粗测）。
2. ReadDir(2) 分页读大目录，验证拼接结果与一次读完一致；故意不读完就
   Close，理解 n 参数的用途。
3. 在 goroutine 里 Chdir，主 goroutine 同时用相对路径写文件，复现
   「Chdir 是进程全局状态」的坑；改用 filepath.Join(absRoot, name) 解决。

读完后试着回答：**现在能列一层目录了，怎么遍历整棵树？怎么把 `a/./b/../c`
这种路径归一化、怎么判断两个相对路径谁大谁小？** 下一课学习路径处理与 WalkDir。

## 验收标准

- `go test ./...` 通过。
- 能说清什么时候用 DirEntry、什么时候必须 Stat。
- 能解释为什么 Remove 一个非空目录会失败、MkdirAll 为什么不报错。
- 能说出 Chdir 在并发程序里的危险。
