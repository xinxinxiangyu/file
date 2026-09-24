# 第 6 课：原子更新、fsync 与文件锁

> 状态：计划稿（学到本课时再补完整讲解）

## 这一课解决什么问题

第 5 课看到 rename 是原子的，这一课把这个性质用起来，解决三个工程问题：
怎样更新文件才能让任何读者（以及崩溃恢复后的进程）永远看不到「半截内容」；
怎样保证 Write 返回成功的数据真的在物理介质上；多个进程/ goroutine 同时
操作同一文件时怎样协调。这是文件 API 里的「可靠性」一课，地位等同 net 的
TLS/超时/取消。

## 先跑起来

```sh
go run ./cmd/atomic config.json '{"version":1}'
go run ./cmd/atomic config.json '{"version":2}'   # 内容整体替换，从不出现半截 JSON

go run ./cmd/lock counter.dat   # 终端 A：持续 +1
go run ./cmd/lock counter.dat   # 终端 B：同时运行，计数串行不丢
```

## 核心概念

1. **原子替换四步**（写配置、换日志、软件更新都是这个套路）：

   ```text
   1. 在同目录创建临时文件（os.CreateTemp，同目录才能保证同设备 rename）
   2. 写入全部内容
   3. f.Sync() 等待落盘
   4. os.Rename(tmp, target) 覆盖；必要时再 Sync 父目录
   ```

   关键：rename 由文件系统保证原子；而**直接打开目标文件从头覆写**时，
   读者可能恰好看到写了一半的内容，崩溃后更是直接损坏。
2. **O_CREATE|O_EXCL**：创建时要求「此前必须不存在」，否则失败——
   实现「谁先抢到谁创建」的单实例哨兵文件、锁文件，防止静默覆盖。
3. **fsync 的层次**：Write 成功只代表进内核页缓存；`f.Sync()`（fsync，
   macOS 上底层是 F_FULLFSYNC）等数据+元数据落盘；rename 后还要对父目录
   打开后 Sync，目录项的新名字才算落盘。
4. **咨询文件锁**（`syscall.FcntlFlock`，对应 fcntl 记录锁；macOS 也可用
   `syscall.Flock_t`）：
   - 读共享锁（RLock，多人可持）/ 写排他锁（WLock，独占）；
   - 只在**所有参与者都主动加锁**时有效——它不阻止没加锁的进程读写，
     所以叫「咨询锁」；
   - `F_SETLK` 抢不到立刻返回错误，`F_SETLKW` 阻塞等待；进程退出/Close
     对应描述符后锁自动释放（崩溃也不会留下死锁）。
5. **O_APPEND 与小写入**：O_APPEND 的「定位到末尾 + 写入」对常规文件是
   原子的，适合多进程写日志；但大于 PIPE_BUF 一类边界、以及跨平台行为要
   谨慎，跨记录结构仍建议加锁。
6. **临时文件 API**：`os.CreateTemp`/`os.MkdirTemp` 负责随机不冲突的名字与
   0600 权限；记得 `defer os.Remove(tmp)` 或交给 rename 接管。

## 要写的代码

- `internal/fileutil/atomic.go`：`WriteFileAtomic(name string, data []byte, perm fs.FileMode) error`
  （四步套路，临时文件创建在目标同目录）。
- `internal/fileutil/lock.go`：`Lock(f *os.File) error` / `Unlock`，
  封装 `syscall.FcntlFlock`；`WithLock(path, fn)` 便捷封装。
- 对应 `_test.go`：原子写内容/权限正确、中途模拟失败临时文件被清理；
  同进程两个文件描述符验证排他锁互斥、共享锁共存、解锁后可抢；O_EXCL 冲突。
- `cmd/atomic/main.go`、`cmd/lock/main.go`（锁内读—改—写计数，演示无丢失）。

## 建议阅读顺序

「直接覆写」的事故现场 → 原子替换四步 → 为什么临时文件必须在同目录 →
O_EXCL → 页缓存与 Sync 层次 → FcntlFlock 共享/排他/自动释放 → O_APPEND 边界。

## 自己做三个实验

1. 一个终端每秒 `cat config.json | python3 -m json.tool`（读者），另一个
   终端高频原子写；再改成直接覆写对比——前者从不报错，后者能抓到解析失败。
2. 运行中 `kill -9` 原子写的程序：分别杀在写临时文件后、rename 前、rename
   后，检查目标文件与目录里残留的临时文件，总结每阶段的崩溃结果。
3. 三个终端跑 lock 计数器各几百次，核对最终值；再故意让一个程序不加锁，
   观察计数丢失——用实验证明「咨询锁靠大家守规矩」。

读完后试着回答：**这些 API 都绑死在本地文件系统上；有没有一个标准接口，
让同一套代码既能读本地目录、又能读编译进二进制的静态文件、还能接到 HTTP？**
最后一课进入 `io/fs`。

## 验收标准

- `go test ./...` 通过。
- 能默写原子替换四步并解释每步的理由。
- 能说清 Write 成功与 fsync 返回之间数据可能在哪一层。
- 能用文件锁让多进程计数零丢失，并解释咨询锁的前提。
