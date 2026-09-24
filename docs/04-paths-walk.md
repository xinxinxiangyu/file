# 第 4 课：路径处理与目录树遍历

> 状态：计划稿（学到本课时再补完整讲解）

## 这一课解决什么问题

第 3 课能操作一层目录，这一课处理整棵树和各种路径字符串。两部分内容：
`path/filepath` 负责路径的拼接、归一化、拆分与 glob 匹配（**永远不要手工
拼字符串路径**）；`filepath.WalkDir` 用深度优先的回调模型遍历整棵目录树。
学完后写出三个日常真用得上的工具：`tree`、`du`、`find`。

## 先跑起来

```sh
go run ./cmd/tree .
go run ./cmd/du .
go run ./cmd/find . -name '*.md'
go run ./cmd/find . -type d
```

## 核心概念

1. **filepath 常用函数**：

   | 函数 | 作用 | 示例 |
   | --- | --- | --- |
   | `Join` | 按系统分隔符拼接并清理 | `Join("a", "b", "../c")` → `a/c` |
   | `Clean` | 归一化 `.`/`..`/多余斜杠 | `a/./b//` → `a/b` |
   | `Base/Dir/Ext` | 取文件名/所在目录/扩展名 | `/a/b.md` → `b.md` `/a` `.md` |
   | `Abs/Rel` | 转绝对路径/相对路径 | |
   | `SplitList` | 拆分 PATH 这种冒号列表 | |
   | `Glob` | 单层通配匹配 | `filepath.Glob("*.go")` |
   | `VolumeName` | 平台差异（macOS/Linux 为空，Windows 有盘符） | |

2. **WalkDir 的模型**：

   ```go
   filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error)
   ```

   - 深度优先：先回调父目录，再递归子目录；path 是**拼好的完整路径**。
   - `err` 参数：某个子目录读失败时，错误会作为参数送进来，由你决定
     继续（return nil）还是中止（return err）；不要忽略它。
   - 回调返回 `fs.SkipDir` 可剪枝；这是控制遍历的唯一正规手段。
3. **统计目录大小（du 的关键）**：遍历中所有常规文件的 Size 求和；注意
   硬链接（第 5 课）会让 naive 求和重复计数，先接受这个已知缺陷。
4. **WalkDir vs io/fs 的 WalkDir**：`filepath.WalkDir` 走真实文件系统；
   `fs.WalkDir(fsys, name, fn)` 走任意 `fs.FS`——回调签名几乎一样，
   第 7 课会看到同一份遍历逻辑可以跑在磁盘、embed、HTTP 上。
5. **符号链接不跟随**：WalkDir 不会递归进符号链接指向的目录（避免环），
   链接本身作为一个条目出现；需要跟随得自己在回调里 Stat + 处理。
6. **错误处理**：路径不存在时 WalkDir 用第一个回调的 err 告诉你；
   权限不足的目录表现为回调里的 ErrPermission。

## 要写的代码

- `internal/walkx/walkx.go`：`Tree(w, root)`、`DirSize(root) (int64, int64, error)`
  （总字节、文件数）、`Find(root, matcher) ([]string, error)`、`MatchName(pattern)`。
- `internal/walkx/walkx_test.go`：在 `t.TempDir()` 构造多层树（含空目录、
  深层嵌套、权限受限目录），断言 tree 行数、du 汇总、find 模式匹配与
  SkipDir 剪枝、错误目录不崩溃。
- `cmd/tree`、`cmd/du`、`cmd/find` 三个 main。

## 建议阅读顺序

filepath 函数表（每个都在终端用 Go 小片段试一遍）→ WalkDir 回调签名与
err 参数 → SkipDir → 三个工具的实现 → fs.WalkDir 的对照预告。

## 自己做三个实验

1. 给 find 加 `-maxdepth N`：用 strings.Count 路径分隔符或记录深度
   （WalkDir 不直接给深度）实现剪枝。
2. 遍历 `/usr/bin` 或 `/System` 下的大目录，对比 naive du 与系统 `du -k`
   的数字差异，思考块大小、硬链接、压缩文件系统各造成什么差异。
3. 构造一个「名字里带换行/空格」的文件（`touch $'a\n b'`），看 naive
   按行输出的工具会怎样错乱——体会为什么真实工具需要 `-print0` 式设计。

读完后试着回答：**du 里同一个文件被硬链接多次会重复计数——“同一个文件”
到底怎么判定？文件名和文件是什么关系？** 下一课用链接 API 揭晓。

## 验收标准

- `go test ./...` 通过。
- 三个工具在任意目录上行为与系统 tree/du/find 大体一致。
- 能解释 WalkDir 回调里 err 参数的意义和 SkipDir 的用法。
- 能手算 Clean、Join 的结果。
