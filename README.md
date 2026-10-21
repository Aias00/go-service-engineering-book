# Go 服务工程：并发、资源管理与故障诊断

面向已经会写Go HTTP接口的后端开发者，围绕真实HTTP服务与后台worker，讨论取消传播、资源所有权、有界接纳、慢客户端、连接与响应体、SIGTERM和故障诊断。

作者：Aias00，Apache ShenYu PMC成员，Go / Java / Agent基础设施开发者。

- [在线阅读](https://aias00.github.io/go-service-engineering-book/)
- [EPUB下载](https://aias00.github.io/go-service-engineering-book/downloads/go-service-engineering-book.epub)
- [独立实验ZIP](https://aias00.github.io/go-service-engineering-book/downloads/lab.zip)
- [版本发布](https://github.com/Aias00/go-service-engineering-book/releases)

18章正文、前言、3个手写附录与1个构建时生成的完整代码附录。正文超过26000汉字，代码和附录不计入门槛。默认浅色，支持共享深色主题、目录、全文搜索、字号与无JavaScript阅读；正文同源生成网页、EPUB和离线HTML。

## 内容

1. 服务运行契约
2. context取消协议
3. goroutine与资源所有权
4. channel、锁和可见性
5. 有界并发与过载
6. 时间预算和重试
7. HTTP输入边界
8. 流式响应与慢客户端
9. Transport与响应体
10. 后台任务生命周期
11. 有截止的优雅退出
12. 并发行为测试
13. race detector
14. CPU、分配与存活堆
15. goroutine、阻塞与锁诊断
16. 故障排查流程
17. 基准与容量边界
18. 可持续交付

## 运行实验

本机实测Go 1.24.3 darwin/arm64；CI固定Go 1.24.3 Linux。只依赖标准库。需要支持race的系统、C编译器与CGO；SIGTERM测试面向Unix语义。

```sh
cd lab
go test -race ./...
go vet ./...
go run ./cmd/service
```

实验ZIP解压后go.mod位于根目录，可以直接执行 `go test -race ./...`。详细配置、重复故障命令和pprof方法见 [实验说明](lab/README.md)，实际输出见 [实测记录](lab/EXPERIMENTS.md)。

已执行真实TCP连接耗尽与恢复、阻塞Body取消、慢客户端不读、过载接纳、真实子进程SIGTERM。已运行race、故障组30轮、信号组10轮、三次基准和实际profile。基准只代表本机局部工作池成本，不是生产容量。202只表示进程内接纳，任务不持久化；代码不提供业务身份、URL出站策略或跨进程幂等。

## 构建与校验

需要Python 3、Pandoc 3.12.1、Java 21；浏览器检查使用Playwright 1.64.1。

```sh
python3 scripts/check-manuscript.py
python3 scripts/build.py
python3 scripts/verify.py
sh scripts/epubcheck.sh
```

EPUBCheck固定5.4.0，要求零错误、零警告。完整网页与EPUB逐章文本、目录、搜索和代码ZIP由脚本核验。发布附件使用成功CI的原始artifact，公网哈希与交付证据见 [DELIVERY.md](DELIVERY.md)。官方来源见 [来源附录](book/chapters/20-sources.md)，原始官方文档缓存不公开。

原创书稿保留所有权利；程序、构建和网页代码采用MIT许可，见 [LICENSE](LICENSE)。
