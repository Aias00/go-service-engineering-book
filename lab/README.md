# Go 服务工程实验

《Go 服务工程：并发、资源管理与故障诊断》的独立实验工程。只依赖 Go 标准库，实测 Go 1.24.3；在 macOS/Linux 上解压本目录后直接执行：

```sh
go test -race ./...
```

Race 检测需要支持的平台、C 编译器和 `CGO_ENABLED=1`。普通测试可执行 `go test ./...`。进程集成测试发送真正的 SIGTERM，因此需要 Unix 信号语义。

## 启动与请求

```sh
go build -o /tmp/go-service-lab ./cmd/service
/tmp/go-service-lab
```

在另一个终端执行：

```sh
curl -i -X POST 'http://127.0.0.1:8080/tasks?delay=250ms'
curl 'http://127.0.0.1:8080/stats'
curl 'http://127.0.0.1:8080/work?delay=250ms'
curl -o /dev/null 'http://127.0.0.1:8080/stream?bytes=1048576'
curl -i -X POST 'http://127.0.0.1:8080/tasks?url=https%3A%2F%2Fexample.com'
```

`POST /tasks` 返回 202 和任务 ID；这是进入内存队列的确认，不是任务完成或持久化的确认。队列已满或已经关闭时返回 503 和 `Retry-After: 1`。ID 允许有空号。`GET /work` 在当前请求中等待；`GET /stream` 写指定字节数，默认 64 MiB，最大 256 MiB。`GET /healthz` 检查 HTTP 存活。`/stats` 是多个原子计数器的近似同时快照，运行中不保证跨字段等式；停稳后的计数可用于核对接收、完成和释放边界。

| 环境变量 | 默认值 | 含义 |
| --- | --- | --- |
| `LISTEN_ADDR` | `127.0.0.1:8080` | 业务 HTTP 监听地址；`:0` 分配临时端口 |
| `ADMIN_ADDR` | `127.0.0.1:6060` | 独立 pprof 监听地址 |
| `WORKERS` | `2` | 固定后台 worker 数 |
| `QUEUE_CAPACITY` | `8` | 等待队列上限，不含正在执行的任务 |
| `TASK_TIMEOUT` | `5s` | worker 开始执行后每个任务的超时 |
| `STREAM_WRITE_TIMEOUT` | `250ms` | 整段流式响应的绝对写截止 |
| `SHUTDOWN_TIMEOUT` | `5s` | HTTP 请求排空和后台任务排空共享的预算 |

将 `WORKERS=1 QUEUE_CAPACITY=1` 设置后并发提交多个 `delay=5s` 任务，可观察过载拒绝。实验的 URL 参数允许访问调用者提供的 HTTP(S) 地址；服务和 pprof 默认只监听本机。本工程不包含身份认证、URL 出站策略和磁盘队列。

## 所有权与退出约定

- `Pool.Submit(ctx, Work)` 中的 `ctx` 只控制入队。任务一旦接受，就由 Pool 的根 context 和每任务截止管理。任务闭包只保存参数，不保存请求、ResponseWriter 或请求 context。
- Pool 用固定 worker 和有界 channel 控制并发；入队时使用 `select/default`，不等待队列空位。并发关闭与入队由同一个锁保护。`Close` 停止接收并排空队列，`Shutdown(ctx)` 在预算用尽后发出取消，`Done()` 表示 worker 已经实际返回。
- 任务超时从执行开始计算，不包含排队时间。被强制取消的排队任务会被 worker 取出并记为取消完成，不会调用业务函数。`Work` 必须响应 context 并释放自己的资源；Go 无法强杀一个忽略取消的 goroutine。任务 panic 会传播并终止进程。
- SIGTERM/SIGINT 先停止 HTTP 监听并等待现有 handler，再关闭任务入口并排空已接受任务。共享预算到期后关闭 HTTP 连接、取消任务，然后额外等待最多 1 秒合作清理。`App.DrainRequests(ctx)` 显式等待 handler 返回，因为 `http.Server.Close` 本身不等待 handler；它先关闭 handler 入口，再等待 WaitGroup，避免零计数时 Add 与 Wait 的竞态。超出预算以非零状态退出；正常排空为零。
- Fetcher 的 Client/Transport 全程共享。所有响应体只有一个关闭者；小响应包括错误状态都读到 EOF 再 Close，以复用连接。响应最多消费 1 MiB+1 字节，超限关闭响应体并放弃该连接的复用。连接数、拨号、TLS 握手、响应头等待和任务总时长分别有界。
- `/stream` 使用 `http.ResponseController.SetWriteDeadline` 为整个流设绝对截止。HTTP/1.1 响应设置 `Connection: close`，让截止继续覆盖 net/http 的最后一次缓冲刷新；不在 handler 返回前清除截止。普通路由继续使用 keep-alive。这里的时间限制是整段流上限，不是每次成功写入后的空闲计时。

## 可重复故障实验

```sh
go test -race ./internal/service -run 'Test(PoolQueueBound|PoolBlockedCancellationReleases|PoolConcurrentCloseAndSubmit|ConnectionExhaustionAndRecovery|FetcherBlockedBodyCancellationReleasesConnection|SlowStreamDeadlineReleasesHandler|DrainRequestsWaitsForHandlerAndGatesAdmission)$' -count=30 -timeout=60s
go test -race ./cmd/service -run TestSIGTERM -count=10 -timeout=60s
go test ./internal/service -run '^$' -bench BenchmarkPoolSubmit -benchmem -count=3
```

所有故障测试有自己的等待上限。连接耗尽测试使用真实 TCP：故意占住一个未读响应体，让后续请求等待连接至 context 超时，然后关闭响应体并验证恢复。另一个测试让响应体读取阻塞，验证取消传播到服务端并恢复连接槽。慢客户端测试发送请求后完全不读 256 MiB 响应，验证写截止使 handler 返回。子进程测试直接运行生产 `main` 和 signal handler，检查在途请求、运行任务、排队任务，以及截止后请求/任务取消后的实际释放。

基准的每个 op 都最终成功入队；队列满时 `runtime.Gosched()` 重试的成本也计算在内。它测量包含 worker/context/日志格式化的池吞吐，不代表 HTTP 服务吞吐或单次 Submit 的延迟。结果、机器信息及采样摘要见 [EXPERIMENTS.md](EXPERIMENTS.md)。

## pprof

运行服务时可保存在线采样：

```sh
mkdir -p profiles
curl -o profiles/live.cpu.pprof 'http://127.0.0.1:6060/debug/pprof/profile?seconds=10'
curl -o profiles/live.heap.pprof 'http://127.0.0.1:6060/debug/pprof/heap?gc=1'
curl -o profiles/live.goroutine.txt 'http://127.0.0.1:6060/debug/pprof/goroutine?debug=2'
go tool pprof -top -inuse_space profiles/live.heap.pprof
```

采 CPU 时在另一个终端运行负载；空闲服务的 profile 可能几乎没有业务样本。代码包附带可读 goroutine 快照、profile 摘要和本次采样的 SHA256，二进制 profile 由以下命令重新生成：

```sh
go test ./internal/service -run '^$' -bench BenchmarkPoolSubmit -benchtime=2s -benchmem -cpuprofile profiles/pool.cpu.pprof -memprofile profiles/pool.heap.pprof -o /tmp/go-service-profile.test
go tool pprof -top profiles/pool.cpu.pprof
go tool pprof -top -alloc_space profiles/pool.heap.pprof
```

`alloc_space` 是累计分配采样，`inuse_space` 是采样时仍存活的堆，不能混用。采样受平台、时长和采样率影响；小负载的 heap profile 可能只显示 runtime，不能据此断言业务没有分配。

源代码及随附文档按 [MIT License](LICENSE) 发布。
