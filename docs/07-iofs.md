# 第 7 课：io/fs 接口与生态

> 状态：计划稿（学到本课时再补完整讲解）

## 这一课解决什么问题

前 6 课的代码都直接依赖 `os`，绑死在本地文件系统。最后一课学习 Go 的
`io/fs` 抽象（地位等同 net 项目接入 `database/sql/driver`）：一套极小的
接口把「文件系统」从具体存储里解放出来。同一份业务代码可以跑在
`os.DirFS`（真实目录）、`embed.FS`（编译进二进制的文件）、`fs.Sub`
（子树）上，`fs.WalkDir`、`fs.Glob`、`fs.ReadFile` 和 `http.FileServerFS`
全部通用。这是从「会调 API」走向「理解 API 为什么这样设计」的一课。

## 先跑起来

```sh
go run ./cmd/fsview        # 同一段代码分别读磁盘目录和 embed 嵌入文件，输出一致
go run ./cmd/fshttp        # http.FileServerFS 把当前目录暴露在 http://127.0.0.1:8080
```

fsview 内嵌一份 `web/` 静态资源（go:embed），运行时再和磁盘上的同名目录
对比遍历结果；fshttp 浏览器里可以直接浏览目录、下载文件。

## 核心概念

1. **接口阶梯**（都是小接口，按需实现、按需断言）：

   ```text
   fs.FS          Open(name string) (fs.File, error)
   fs.File        Stat() (FileInfo, error)  Read([]byte) (int, error)  Close() error
   fs.ReadDirFile 在 fs.File 上加 ReadDir(n int) ([]DirEntry, error)
   fs.ReadFileFS  ReadFile(name string) ([]byte, error)
   fs.ReadDirFS   ReadDir(name) ([]DirEntry, error)
   fs.StatFS      Stat(name) (FileInfo, error)
   fs.GlobFS      Glob(pattern)
   ```

   设计哲学：最小接口 `FS+File`，能力通过类型断言（`fsys.(fs.ReadDirFS)`）
   逐层加上——和 `io.Reader/ReaderAt/Closer` 是同一种美学。
2. **fs 的路径规则**：一律用 UTF-8 的 `/` 分隔（**不是** OS 分隔符），
   根是点号 `"."`，不允许前导 `/`、`//`、`..`；先用 `fs.ValidPath(name)`
   校验。把本地路径喂给 fs.FS 前要去掉前导斜杠。
3. **os.DirFS**：`fsys := os.DirFS("/etc")` 把真实目录包成 fs.FS；
   返回的文件、目录都是前几课认识的 os 类型，只是换了统一入口。
4. **embed.FS**：

   ```go
   //go:embed web
   var assets embed.FS
   ```

   编译时把 `web/` 目录打进二进制，运行时它也是一个 fs.FS——只读、无权限
   概念、部署单个二进制即可分发静态资源。
5. **fs.Sub 与组合**：`sub, _ := fs.Sub(fsys, "web/static")` 得到子树视图，
   之后所有路径相对子树；可用于给 HTTP 只暴露某个目录（安全边界）。
6. **白送的函数**：`fs.WalkDir`、`fs.Glob`、`fs.ReadFile`（会断言
   ReadFileFS，没有就退回 Open+Read）、`fs.Stat`——与第 4 课 filepath
   版本签名几乎一致，换的只是数据源。
7. **HTTP 收尾**：Go 1.22 的 `http.FileServerFS(fsys)` 一行接 Web；
   至此「本地目录 / 嵌入资源 → fs.FS → HTTP → 网络」形成闭环，
   正好接回 net 项目的世界。

## 要写的代码

- `internal/fsapp/fsapp.go`：只依赖 fs.FS 的业务函数：
  `List(fsys, dir)`、`CountFiles(fsys) (int, error)`（fs.WalkDir）、
  `FindMD(fsys) ([]string, error)`（fs.Glob）、`SHA256(fsys, name)`。
- `internal/fsapp/fsapp_test.go`：用 `fstest.MapFS`（标准库提供的内存假 FS，
  可精确控制条目和错误）测业务逻辑；同一份测试用 `os.DirFS(t.TempDir())`
  再跑一遍，验证抽象对两种后端成立。
- `cmd/fsview/main.go`：go:embed web 目录，对 os.DirFS 与 embed.FS 各跑一遍
  fsapp 函数并对比。
- `cmd/fshttp/main.go`：fs.Sub 取暴露子树 + http.FileServerFS。

## 建议阅读顺序

接口阶梯图 → 为什么 FS 只有一个方法 → ValidPath 规则（与 filepath 的差异）→
os.DirFS → go:embed/embed.FS → fs.Sub → 白送函数 → MapFS 测试 →
FileServerFS 收尾。

## 自己做三个实验

1. 用 fs.Glob 在磁盘目录和 embed.FS 上分别找 `**/*.md` 风格文件
   （注意 Glob 不跨目录递归，要递归得 WalkDir + filepath.Match）。
2. 给 fsapp 传一个 fstest.MapFS，其中某个目录的 ReadDir 返回构造的错误，
   验证你的错误处理与第 4 课 WalkDir 的处理一致。
3. fshttp 分别暴露整个目录和 fs.Sub 出的子目录，尝试用 `..` 或绝对路径
   URL 逃逸子树——观察 ValidPath/Sub 如何把根限制住（对比自己写拼接路径的危险）。

## 课程结束后的延伸方向

- 阅读 `testing/fstest` 源码：理解一个「合规文件系统」应满足哪些可测试约定。
- 自己实现一个最小的 fs.FS（如把 zip 包 `archive/zip` 的 Reader 适配为 fs.FS）。
- 第三方 fs.FS 生态：`github.com/hanwen/go-fuse`、s3 后端等，看接口如何贯通。
- 回到 OSTEP：把这 7 课用过的概念（权限、目录、inode、链接、崩溃一致性）
  对照文件系统实现章节读一遍，你会发现每个 API 行为都有磁盘上的理由。

## 验收标准

- `go test ./...` 通过。
- fsview 中同一份业务代码在 os.DirFS 与 embed.FS 上结果一致。
- 浏览器/curl 能通过 fshttp 浏览目录与下载文件。
- 能说清 fs.FS 路径与 OS 路径的 3 点差异（分隔符、根、ValidPath）。
