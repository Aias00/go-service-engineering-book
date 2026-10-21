# 实测记录

采集日期：2026-10-21，约 10:23—10:28 UTC。环境：`go version go1.24.3 darwin/arm64`，Apple M4，`CGO_ENABLED=1`。基准后缀 `-10` 表示当次 GOMAXPROCS=10。以下是本次实际输出，不是性能目标；读者机器上的数值会不同。

## 正确性与故障重复

```text
$ go test -race ./...
ok  example.com/go-service-engineering/lab/cmd/service       2.981s
ok  example.com/go-service-engineering/lab/internal/service  1.901s

$ go vet ./...
[无输出，退出状态 0]

$ go test -race ./internal/service -run 'Test(PoolQueueBound|PoolBlockedCancellationReleases|PoolConcurrentCloseAndSubmit|ConnectionExhaustionAndRecovery|FetcherBlockedBodyCancellationReleasesConnection|SlowStreamDeadlineReleasesHandler|DrainRequestsWaitsForHandlerAndGatesAdmission)$' -count=30 -timeout=60s
ok  example.com/go-service-engineering/lab/internal/service  8.224s
```

每轮涵盖：固定上限、满队列拒绝、阻塞任务到期释放、并发 Submit/Close、真实 TCP 连接占满后超时及恢复、读响应体阻塞时取消释放、完全不读响应的慢客户端写截止、handler 排空入口和等待。共 30 轮，测试本身的等待也有上限。

`cmd/service` 的两个 SIGTERM 测试分别检查：3 秒预算内，一个 150ms 在途 HTTP 请求、350ms 运行任务和 150ms 排队任务全部排空；80ms 预算内，一个 5s 请求和 5s 任务被取消，handler 和 worker 均释放，进程非零退出。重复执行命令：

```sh
go test -race ./cmd/service -run TestSIGTERM -count=10 -timeout=60s
```

最终输出：

```text
ok  example.com/go-service-engineering/lab/cmd/service  17.458s
```

## 基准

```text
$ go test ./internal/service -run '^$' -bench BenchmarkPoolSubmit -benchmem -count=3
goos: darwin
goarch: arm64
pkg: example.com/go-service-engineering/lab/internal/service
cpu: Apple M4
BenchmarkPoolSubmit-10      1279885       950.9 ns/op     304 B/op     8 allocs/op
BenchmarkPoolSubmit-10      1000000        1218 ns/op     304 B/op     8 allocs/op
BenchmarkPoolSubmit-10       910029        1176 ns/op     304 B/op     8 allocs/op
PASS
ok  example.com/go-service-engineering/lab/internal/service  3.830s
```

每个 op 都成功接受一个空任务；满队列重试、四个 worker、每任务 timeout context 和同步日志格式化都包含在测量中。日志实际写入 `io.Discard`，不包含终端或磁盘 I/O。基准结束后等待所有已接受任务退出。三次波动不能推出容量保证、HTTP p99 或线上性能。

## 实际 profile 摘要

```text
$ go test ./internal/service -run '^$' -bench BenchmarkPoolSubmit -benchtime=2s -benchmem -cpuprofile profiles/pool.cpu.pprof -memprofile profiles/pool.heap.pprof -o /tmp/go-service-engineering-profile.test
BenchmarkPoolSubmit-10      2343188       974.5 ns/op     305 B/op     8 allocs/op
PASS
ok  example.com/go-service-engineering/lab/internal/service  2.745s
```

本次采集的 `pool.cpu.pprof` 的 `go tool pprof -top -nodecount=12` 摘录：

```text
Duration: 2.41s, Total samples = 8.56s (355.91%)
flat     flat%    function
4.09s    47.78%   runtime.usleep
1.71s    19.98%   runtime.pthread_cond_wait
0.49s     5.72%   runtime.pthread_cond_signal
0.40s     4.67%   runtime.madvise
0.11s     1.29%   runtime.walltime
0.09s     1.05%   internal/sync.(*Mutex).Unlock
```

多核样本总量可以超过墙钟时长。这份 Darwin 样本主要落在 runtime/系统等待栈；不能据此把 `usleep` 当作业务 CPU 热点或给出锁优化结论。应结合更长采样、调用栈、block/mutex profile 和业务负载再判断。

本次采集的 `pool.heap.pprof` 的 `-alloc_space`：

```text
Showing nodes accounting for 663.06MB, 98.71% of 671.74MB total
flat        flat%    cum        function
344.03MB    51.21%   598.56MB   context.WithDeadlineCause
254.53MB    37.89%   254.53MB   time.newTimer
38MB         5.66%   651.06MB   service.(*Pool).run
13.50MB      2.01%   15MB       service.(*Pool).Submit
13MB         1.94%   13.50MB    fmt.Sprintf
```

这里是约 234 万个任务累积分配的采样量，绝不是 671.74MB 存活堆或泄漏证据。每任务 deadline context/timer 是此次累计分配的主要来源。

在线 profile 从实际编译的服务、真实 HTTP 管理端口采集：先提交两个 `delay=5s` 任务并等待 active=2，再提交八个任务；请求 `/debug/pprof/goroutine?debug=2` 和 `/debug/pprof/heap?gc=1`，分别保存为 `profiles/live.goroutine.txt`、`profiles/live.heap.pprof`。

```text
LIVE STATS {accepted:10 started:2 completed:0 failed:0 cancelled:0 active:2 queued:8}
SIGTERM, SHUTDOWN_TIMEOUT=100ms
shutdown complete {accepted:10 started:10 completed:10 failed:10 cancelled:10 active:0 queued:0}
process exit: 1, error: context deadline exceeded
```

取消后的 `started=10` 表示十个队列项目均被 worker 处理到完成状态，不代表被取消的八个排队业务函数执行过。goroutine 文本可直接定位两个 worker 的 `service.wait`/`select` 栈；它是单时刻栈快照。最终在线 heap 样本约 2051 KiB，主要为 runtime 与压缩编码初始化分配；小规模业务对象未必被默认采样率抽中。读取方法与重采命令见 README。

发布包包含摘要和 `profiles/live.goroutine.txt`；二进制采样保存在本次构建的本地 evidence，不随代码包发布。以下 SHA256 标识本次二进制采样，因时间戳和运行调度不同，重采不要求散列相同：

| 文件 | SHA256 |
| --- | --- |
| `pool.cpu.pprof` | `3efac2e4630bebfe7f81352fc2b2e3d17d6e343a602a8f4115853709d9023121` |
| `pool.heap.pprof` | `2deb44a62a2ab8b85aa6ef6af5c6b0c5163ff2d0787322ccc174144794266132` |
| `live.heap.pprof` | `5202267c274e0f830baf32e0328f0a210d94af6acac2845e74f37a84080b6f18` |

CI 应重新采样并验证 pprof 可以解析，而不检查固定百分比：

```sh
mkdir -p evidence
go test ./internal/service -run '^$' -bench BenchmarkPoolSubmit -benchtime=2s -benchmem -cpuprofile evidence/ci.cpu.pprof -memprofile evidence/ci.heap.pprof -o evidence/service.test
go tool pprof -top evidence/ci.cpu.pprof
go tool pprof -top -alloc_space evidence/ci.heap.pprof
```
