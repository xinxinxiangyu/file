# 第 2 课：文件属性与权限

> 配套代码：[cmd/finfo/main.go](../cmd/finfo/main.go)（ls -l 风格工具）、
> [internal/finfo](../internal/finfo/finfo.go)（权限串、八进制格式化与测试）。

## 这一课解决什么问题

第 1 课只看文件内容。文件系统还为每个文件保存一份「档案」：类型、大小、
权限位、属主属组、时间戳、inode 号……这一课通过 `os.Stat`（对应系统调用
`stat(2)`）拿到这份档案，像 `ls -l` 一样解读它，亲手改权限并观察「权限被
拒绝」的错误，从而理解 UNIX 的自主访问控制（DAC）模型：**文件的主人定规则，
内核在每次访问时逐条核对。**

对应 net 项目：PostgreSQL 启动握手是「向对端询问状态」，而 Stat 是**向文件
系统取回一个文件的状态报告**——没有对端，只有一份内核代写的档案。

## 心智模型：目录项、inode 与「状态报告」

第 1 课说过，路径逐级解析最终找到 inode。文件系统里每个名字背后的结构是：

```text
  目录项（dentry / 目录里的一条记录）
  ┌──────────────────────────┐
  │ 名字 "notes.txt"         │
  │ → 指向 inode 21341       │
  └──────────────────────────┘                  inode 21341
                                       ┌────────────────────────────┐
                                       │ 类型、权限、属主属组        │
                                       │ 大小、时间戳、硬链接数      │
                                       │ → 数据块位置                │
                                       └────────────────────────────┘
```

三个要点：

1. **名字和档案是分开存的**。名字在目录里，档案（元数据）在 inode 里；
   所以多个名字可以指向同一个 inode（硬链接，第 5 课），也解释了为什么
   「改权限」对所有名字同时生效。
2. **stat 不打开文件、不读内容**，它只是让内核把 inode 上的元数据抄一份
   给你。所以文件权限是 0000 时 `cat` 被拒，`finfo` 依然能显示它——
   stat 只需要**父目录的可穿过权限 x**，不需要文件自己的 r。
3. **路径上的每一级目录都要有 x**。要访问 `/a/b/c`，进程对 `/a`、`/a/b`
   都必须有 x（以及按操作不同需要 r/w），检查在路径解析的沿途逐级进行。

## 先跑起来

```sh
go run ./cmd/finfo .
go run ./cmd/finfo -stat /etc/hosts
go run ./cmd/finfo -octal /etc/hosts
```

实际输出（用户名/组名因机器而异）：

```text
$ go run ./cmd/finfo -stat /etc/hosts
-rw-r--r--   1 root     wheel         213 Feb  5 13:13 hosts
    inode=21341 设备 dev=16777232 rdev=0 原始模式=0100644 硬链接数=1 UID=0 GID=0

$ go run ./cmd/finfo -octal /etc/hosts
0644 /etc/hosts
```

给目录时，第一行是目录自己，随后逐条列出其中的条目（与 `ls -l` 同序）。

## 逐个 API 读

### 1. os.Stat 与 FileInfo

`os.Stat(name)` 返回 `os.FileInfo` 接口：

| 方法 | 含义 |
| --- | --- |
| `Name()` | 文件名（不含目录部分） |
| `Size()` | 字节数（常规文件；其他类型含义不同） |
| `Mode()` | `fs.FileMode`：**类型位 + 权限位**打包在一起 |
| `ModTime()` | 最后修改时间（`time.Time`） |
| `IsDir()` | 是否目录（等价 `Mode().IsDir()`） |
| `Sys()` | 底层系统结构：Unix 上是 `*syscall.Stat_t`，含 Ino、Uid、Gid、Nlink |

`FileInfo` 是接口，所以它不绑定磁盘：`*syscall.Stat_t` 才是内核原始结构，
中间由 `os` 包做了一层适配。想拿 inode 号等接口之外的字段，就向 `Sys()` 要。

### 2. Mode：权限串与八进制换算

`-rw-r--r--` 分四部分读：

```text
-       rw-       r--       r--
类型    属主      属组       其他人
```

- 类型：`-` 常规文件、`d` 目录、`l` 符号链接，还有 `p`/`s`/`b`/`c`。
- 每个位置只能是 `r`/`w`/`x`/`-`；三个比特的权值固定：

```text
r = 4    w = 2    x = 1
```

  所以一组 `rw-` = 4+2+0 = 6，`r--` = 4，`rw-r--r--` = **0644**；
  看到 `rwxr-x---` 要能立刻报出 **0750**。

[internal/finfo/finfo.go](../internal/finfo/finfo.go) 的 `FormatMode` 就是
把 9 个权限比特从高位（0400）到低位（0001）映射成这串字符；`OctalMode`
返回 `chmod` 使用的那个八进制数。

### 3. 特殊位：setuid / setgid / sticky

权限串里有三个不那么显眼的位（八进制 04000/02000/01000）：

| 位 | ls 中的位置 | 作用 |
| --- | --- | --- |
| setuid（04000） | 属主 x 变成 `s`/`S` | 执行时暂时获得文件**属主**的身份（如 `passwd` 命令） |
| setgid（02000） | 属组 x 变成 `s`/`S` | 执行时获得属组身份；目录里还会让新文件继承目录属组 |
| sticky（01000） | 其他人 x 变成 `t`/`T` | 目录里只能删自己的文件（如 `/tmp`） |

对应位置原本有 x 显示小写（`s`/`t`），没有 x 显示大写（`S`/`T`）。

平台事实（写测试时踩到的坑，值得记住）：**Go 把这三个位放在 `fs.ModeSetuid`
等高位标志里**，`m.Perm()` 只给低 9 位，所以 `OctalMode` 要手动把它们折叠
回 04000/02000/01000。另外在 **macOS 上，非 root 用户调 `chmod` 设置这些位
会被内核静默清除**（Linux 普通用户则可以设置）；`finfo_test.go` 里有一个
darwin 专属测试把这个行为钉死。

### 4. 目录的 rwx 是另一套语义（本课重点）

直觉上「r 能读、w 能写、x 能执行」对文件成立，但**目录不是数据文件，是一张
名字表**，三位各管一件事：

| 位 | 对普通文件 | 对目录 |
| --- | --- | --- |
| r | 读内容 | **列出**目录里有哪些名字 |
| w | 改内容 | 在目录里**增、删、改名**（与文件本身权限无关！） |
| x | 执行程序 | **穿过**该目录去访问里面的路径（又叫 search 位） |

亲手做的实验（`$D/sub` 是一个目录，里面有 `inside.txt`）：

```text
$ chmod 000 "$D/sub"
$ ls "$D/sub"                 # 没 r：列不出
ls: ...: Permission denied
$ cat "$D/sub/inside.txt"     # 没 x：穿不过去
cat: ...: Permission denied

$ chmod 100 "$D/sub"          # 只给 x
$ ls "$D/sub"                 # 仍没 r：还是列不出
ls: ...: Permission denied
$ cat "$D/sub/inside.txt"     # 有 x：可以穿过并打开文件
secret
```

这个对比把 r 和 x 彻底分开了：**知道名字、有 x，就能直接访问；想「看看里面
都有谁」，才需要 r。** 另一个反直觉点：删除一个文件看的是**它所在目录的 w**，
不是文件自己的 w——所以 0444 的文件照样能被删（只要目录可写）。

### 5. umask：创建时被屏蔽的位

第 1 课 `OpenFile(..., 0o644)` 的第三个参数是「请求的权限」，但**新建文件的
实际权限 = 请求权限 & ^umask**。umask 是进程的一个屏蔽掩码，`umask` 命令可看：

```text
$ umask
022
```

请求 0666、umask 022 → 实际 0644（组和其他人的 w 被屏蔽）；请求 0600 则不受
影响。Go 程序里没有「改 umask」的封装，需要时用 `syscall.Umask`（通常服务端
会设成 077 再显式 chmod，避免组/其他位泄漏）。

### 6. 改属性：Chmod / Chtimes / Chown

| API | 系统调用 | 改什么 | 谁能改 |
| --- | --- | --- | --- |
| `os.Chmod` | `chmod(2)` | 权限位 | 文件属主或 root |
| `os.Chtimes` | `utimes(2)` | atime/mtime | 属主或 root |
| `os.Chown` | `lchown(2)` | 属主属组 | 通常只有 root |

测试里 `Chtimes` 后再 Stat，`ModTime` 即变成指定时间（注意部分文件系统时间戳
精度只到秒或微秒，断言时按秒截断）。

### 7. 权限错误与哨兵错误；Stat vs Lstat

内核拒绝访问时，Go 返回带 `fs.ErrPermission` 的错误；文件不存在则是
`fs.ErrNotExist`。用 `errors.Is` 区分，**不要匹配错误字符串**：

```go
_, err := os.Stat(path)
switch {
case errors.Is(err, fs.ErrNotExist):
    // 路径不存在
case errors.Is(err, fs.ErrPermission):
    // 权限被拒
}
```

`os.Stat` 会**顺着符号链接**走到目标；`os.Lstat` 看链接本身。本课工具统一用
Lstat，所以 `/var`（macOS 上是指向 `private/var` 的符号链接）显示为：

```text
$ go run ./cmd/finfo -stat /var
lrwxr-xr-x   1 root     wheel          11 Feb  5 13:13 var
    inode=1152921500312599982 ... 原始模式=0120755 ...
```

### 8. Sys() 里的底层字段：inode、设备、链接数

`info.Sys().(*syscall.Stat_t)` 中常用字段：

| 字段 | 含义 | 例子 |
| --- | --- | --- |
| `Ino` | inode 号：文件的唯一身份证（同一文件系统内） | 21341 |
| `Dev` | 所在设备号：文件系统的编号 | 16777232 |
| `Rdev` | 设备文件指向的真实设备（普通文件为 0） | 0 |
| `Nlink` | 硬链接数：有几个名字指向这个 inode | 1 |
| `Uid/Gid` | 属主/属组的数字 ID | 0/0 |
| `Mode` | 原始 st_mode：**类型在高几位八进制** | 0100644 |

`st.Mode` 的类型编码：`0100000` 常规文件、`0040000` 目录、`0120000` 符号
链接——所以 `/etc/hosts` 的 `0100644` = 「常规文件 + 0644」。这些数字第 5
课判断硬链接、识别符号链接时还会用到。

## API → 系统调用 → 文件系统事实

| Go API | Unix 系统调用 | 背后的文件系统事实 |
| --- | --- | --- |
| `os.Stat` | `stat(2)`/`fstatat(2)` | 沿路径解析（逐级查 x），抄写 inode 元数据 |
| `os.Lstat` | `lstat(2)`/`fstatat(AT_SYMLINK_NOFOLLOW)` | 不跟随链接，返回链接自身的档案 |
| `os.Chmod` | `chmod(2)` | 改 inode 上的权限位（属主/root 才行） |
| `os.Chtimes` | `utimes(2)` | 改 inode 上的 atime/mtime |
| `os.Chown` | `lchown(2)` | 改 inode 上的 UID/GID（通常需 root） |
| `fs.ErrPermission` | `EACCES`/`EPERM` | DAC 检查未通过：身份与权限位对不上 |
| `fs.ErrNotExist` | `ENOENT` | 路径解析中某一级没有这个名字 |

## 建议阅读顺序

FileInfo 接口表 → 权限串四部分图解与 4/2/1 换算 →
[finfo.go](../internal/finfo/finfo.go) 的 FormatMode/OctalMode →
目录 rwx 对照实验（000 vs 100）→ umask 与 Chmod/Chtimes →
哨兵错误 ErrPermission/ErrNotExist → Lstat 与 `/var` →
`Sys().(*syscall.Stat_t)` 的 Ino/Nlink/st.Mode；
对照 [finfo_test.go](../internal/finfo/finfo_test.go) 看每个行为如何在
`t.TempDir()` 里被断言。

## 自己做三个实验

1. `touch f && chmod 000 f`：分别用 `cat`、`go run ./cmd/finfo f` 尝试，
   解释为什么一个被拒、一个成功（提示：stat 看的是父目录的什么位？）；
   再 `chmod 644` 恢复。
2. 建目录 `d`，在其中放文件：`chmod 000 d` 时同时尝试 `ls d` 和
   `cat d/里面的文件`；再 `chmod 100 d` 对比；最后试 `chmod 044 d`
   （只有 r）能否 `ls`、能否 `cat`——把三种失败组合凑齐。
3. 用 `-stat` 打印 `/etc/hosts`、`/tmp`、`/var`：记录类型字符、inode 号与
   原始模式高几位（010xxxx/004xxxx/012xxxx）——第 5 课会再次用上。

读完后试着回答：**档案拿到了，但怎么「创建一个目录」、怎么列出目录里都有
谁、DirEntry 和 FileInfo 又是什么关系？** 下一课学习目录 API。

## 验收标准

- `go test ./...` 通过。
- 任意给一个权限串（如 `rwxr-x---`）能立刻报出八进制（0750）。
- 能解释目录的 r、w、x 各自控制什么，并说出删除文件看的是哪个权限。
- 会用 `errors.Is` 判断权限错误与不存在错误。
- 能说清 Stat 与 Lstat 的区别、umask 对新建文件权限的影响。
