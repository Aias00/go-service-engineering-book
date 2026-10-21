《Go 服务工程：并发、资源管理与故障诊断》首次完整版本。

18章正文、前言与4个附录，覆盖取消传播、goroutine所有权、channel/锁、有界接纳、慢客户端、连接与响应体、后台任务、SIGTERM、race、pprof与故障排查。

随书标准库工程在Go1.24.3本机及Linux CI运行race、重复故障与真实子进程信号测试；EPUB、在线阅读、离线HTML与完整Markdown同源。实验ZIP解压后直接go test -race ./...。附件取自成功CI的原始artifact。

202仅表示进程内接纳，不持久化；基准为本机工作池局部成本，不代表生产容量。完整证据见仓库DELIVERY.md。
