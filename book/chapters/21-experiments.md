# 附录B：复现实验与验收清单

## 独立运行入口

下载实验ZIP，解压到空目录，确保go.mod就在当前目录。仓库用户则进入lab。实验只依赖Go标准库；race需要支持的平台、C编译器和CGO。真实信号测试要求Unix语义，因此本书本机与CI分别覆盖macOS arm64、Linux amd64。

```sh
go version
go test -race -v ./...
go vet ./...
```

本机最终复核实际为Go 1.24.3 darwin/arm64。完整race输出包含两个SIGTERM测试和十二个服务行为测试，包级结果为cmd/service 3.075s、internal/service 2.009s。耗时是记录，不是读者机器上的固定断言。全部具体输出同时收录在实验包EXPERIMENTS中。

## 重复故障路径

```sh
go test -race ./internal/service -run 'Test(PoolQueueBound|PoolBlockedCancellationReleases|PoolConcurrentCloseAndSubmit|ConnectionExhaustionAndRecovery|FetcherBlockedBodyCancellationReleasesConnection|SlowStreamDeadlineReleasesHandler|DrainRequestsWaitsForHandlerAndGatesAdmission)$' -count=30 -timeout=60s
go test -race ./cmd/service -run TestSIGTERM -count=10 -timeout=60s
```

本机实际结果分别为8.224s与17.458s，均通过。第一组每轮覆盖队列、取消、并发关闭、连接耗尽恢复、阻塞读体取消、慢读取和handler排空；第二组每轮启动两个真实子进程场景。重复次数不是数学证明，测试本身仍有局部截止与清理。

| 实验 | 故障注入 | 验收不变量 |
| --- | --- | --- |
| 工作池容量 | 阻塞worker并填满队列 | 多余提交立即拒绝，释放后全部终结 |
| 调用者取消 | 接纳成功后取消请求context | 已接纳任务继续由池负责 |
| 任务取消 | 超过任务截止或关闭预算 | Work返回，active最终归零 |
| 连接耗尽 | 保留未读Body占住连接 | 后续请求截止失败，关闭后恢复 |
| 读取取消 | 上游发头后阻塞Body | 取消传到网络，下一请求恢复 |
| 慢客户端 | 真实TCP对端不读256MiB | 写截止触发，handler返回 |
| 正常SIGTERM | 在途请求和运行/排队任务 | 全部排空后进程正常退出 |
| 超时SIGTERM | 五秒请求和任务、80ms预算 | 请求与worker实际释放，进程非零退出 |

## 基准和profile

```sh
go test ./internal/service -run '^$' -bench BenchmarkPoolSubmit -benchmem -count=3
mkdir -p evidence
go test ./internal/service -run '^$' -bench BenchmarkPoolSubmit -benchtime=2s -benchmem -cpuprofile evidence/pool.cpu.pprof -memprofile evidence/pool.heap.pprof -o evidence/service.test
go tool pprof -top evidence/pool.cpu.pprof
go tool pprof -top -alloc_space evidence/pool.heap.pprof
```

三个本机基准分别为950.9、1218、1176 ns/op，均304 B/op和8 allocs/op。每个op最终成功接纳，包含满队列重试、worker、context与日志格式化，日志写入io.Discard。它不代表完整HTTP容量，也不支持生产p99结论。

实际alloc_space采样中context.WithDeadlineCause与time.newTimer占主要部分；CPU采样以运行时等待相关栈为主，不能据此决定业务CPU优化。在线管理端口采样与终结统计的摘要见第15章，原始二进制采样不分发，实验记录保留SHA256供本次证据识别。重采内容受时间与调度影响，不要求哈希相同。

## 按能力验收扩展

给工程增加数据库时，应补连接获取、查询取消与事务结果未知；增加持久任务时，应补领取竞争、崩溃恢复与业务幂等；增加WebSocket时，应补独立关闭通知和等待；增加HTTP/2或代理时，应补流取消与端到端传输。不要用现有测试的通过状态替这些新能力背书。

每次实验记录配置、输入、触发同步点、观察上限和恢复状态。只显示错误而不检查恢复，无法证明资源已经释放；只显示恢复后的成功，也无法证明故障期间守住了容量。两端证据共同构成一个完整实验。
