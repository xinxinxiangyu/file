# 第 2 课：文件属性与权限

> 状态：计划稿（学到本课时再补完整讲解）

## 这一课解决什么问题

第 1 课只会看文件内容。文件系统还为每个文件保存一份「档案」：类型、大小、
权限位、属主属组、时间戳、inode 号……这一课通过 `os.Stat` 拿到这份档案
（对应系统调用 `stat(2)`），像 `ls -l` 一样解读它，亲手改权限并观察「权限
被拒绝」的错误，从而理解 UNIX 的自主访问控制模型。

## 先跑起来

```sh
go run ./cmd/finfo .
go run ./cmd/finfo -stat /etc/hosts
go run ./cmd/finfo -octal /etc/hosts
```

预期：第一行打印类型与权限（如 `-rw-r--r--`）、链接数、属主属组、大小、
修改时间、名字；`-stat` 额外打印底层 inode/设备号；`-octal` 打印如 `0644`。

## 核心概念

1. **FileInfo 接口**（`os.Stat` 返回）：

   | 方法 | 含义 |
   | --- | --- |
   | `Name()` | 文件名（不含目录） |
   | `Size()` | 字节数（常规文件） |
   | `Mode()` | `fs.FileMode`：类型位 + 权限位 |
   | `ModTime()` | 最后修改时间 |
   | `IsDir()` | 是否目录（等价 Mode().IsDir()） |
   | `Sys()` | 底层系统结构：Unix 上是 `*syscall.Stat_t`，含 Ino、Uid、Gid、Nlink |

2. **权限串的读法**：`-rw-r--r--` 分四部分：

   ```text
   -      rw-      r--      r--
   类型   属主     属组     其他人
   ```

   类型：`-` 常规文件、`d` 目录、`l` 符号链接；权限：`r`/`w`/`x`，
   八进制 r=4 w=2 x=1，所以 `rw-r--r--` = 0644。
3. **目录的权限含义不同**：目录的 `r` 是能列条目，`w` 是能在其中增删名字，
   `x` 是能「穿过」它访问里面的路径（所以要访问 `/a/b/c`，对 `/a`、`/a/b`
   都需要 x）。这是最容易踩坑的点。
4. **改属性**：`os.Chmod`、`os.Chtimes`、`os.Chown`（Chown 通常要 root）；
   进程创建文件时权限会受 **umask** 屏蔽（命令行 `umask` 查看）。
5. **权限错误**：被拒绝时 `errors.Is(err, fs.ErrPermission)` 为真；
   不存在时是 `fs.ErrNotExist`——用这两个哨兵错误区分，而不是匹配字符串。
6. **Stat vs Lstat**：`os.Stat` 会顺着符号链接走到目标；`os.Lstat` 看链接
   本身（第 5 课详讲，本课先记住区别）。

## 要写的代码

- `internal/finfo/finfo.go`：`FormatMode(fs.FileMode) string`（权限串）、
  `Describe(os.FileInfo) string`（ls -l 风格一行）、`OctalMode`。
- `internal/finfo/finfo_test.go`：用临时文件 + Chmod 构造 0644/0755/0000/
  目录等情形，断言权限串与八进制；Chtimes 后断言 ModTime。
- `cmd/finfo/main.go`：列目录或对单文件 `-stat`，底层字段从 `Sys().(*syscall.Stat_t)` 取。

## 建议阅读顺序

FileInfo 接口表 → 权限串分组图解 → Mode().Perm() 与八进制换算 →
目录 rwx 的特殊含义 → Sys().(*syscall.Stat_t) 里的 Ino/Uid/Nlink →
ErrPermission / ErrNotExist。

## 自己做三个实验

1. `touch` 一个文件后 `chmod 000`，分别用 cat、我们的 finfo 尝试，再
   `chmod 644` 恢复；再试 `chmod 600` 与 644 的区别（换属主才能体会，
   单机可先读对 umask 的影响）。
2. 建一个目录，`chmod 000 dir` 后尝试列它（需要 r）和 `cat dir/file`
   （需要 x）；再 `chmod 100 dir`（只给 x）对比两种失败。
3. 打印 `/etc/hosts`、`/tmp`、一个符号链接（如 `/var` 下）的 FileInfo，
   记录类型字符和 inode 号——第 5 课会再次用上。

读完后试着回答：**目录的元数据拿到了，但怎么「创建一个目录」、怎么列出
目录里都有谁？** 下一课学习目录 API。

## 验收标准

- `go test ./...` 通过。
- 任意给一个权限串（如 `rwxr-x---`）能立刻报出八进制（0750）。
- 能解释目录的 r、w、x 各自控制什么。
- 会用 errors.Is 判断权限错误与不存在错误。
