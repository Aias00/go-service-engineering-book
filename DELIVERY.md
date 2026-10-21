# 交付证据

《Go 服务工程：并发、资源管理与故障诊断》，作者Aias00，Apache ShenYu PMC成员，Go / Java / Agent基础设施背景。交付日期2026-10-21。

## 版本与公开地址

- [在线阅读](https://aias00.github.io/go-service-engineering-book/)
- [EPUB](https://aias00.github.io/go-service-engineering-book/downloads/go-service-engineering-book.epub)
- [实验ZIP](https://aias00.github.io/go-service-engineering-book/downloads/lab.zip)
- [GitHub仓库](https://github.com/Aias00/go-service-engineering-book)
- [Release v1.0.0](https://github.com/Aias00/go-service-engineering-book/releases/tag/v1.0.0)
- 产品源commit：`c0ba1aa5f6e6ed8a95135e0261808776ea67c20b`
- [成功CI run 39585977252](https://github.com/Aias00/go-service-engineering-book/actions/runs/39585977252)，2026-10-21 10:37:45—10:40:37 UTC，build/deploy均success。
- CI下载artifact：`book-downloads`，ID `12339515928`；Pages artifact ID `12339670711`。Release九个附件直接取自该成功run的原始book-downloads，没有本地重建或替换。

本文件及RELEASE_NOTES不参与产品构建，也不触发Pages；后续仅证据提交不会改变上述产品版本。发布tag固定指向上述成功源commit。

## 完整度与来源

18章正文、前言、3个手写附录、1个构建时生成的完整实验代码附录，共23篇。`python3 scripts/check-manuscript.py`确认排除代码后的正文26712汉字，每章超过1200汉字，且包含练习和实测证据。代码附录和ZIP同源读取13个实验文本文件，含8个Go文件、模块文件、README、MIT许可证、实测记录和goroutine快照；ZIP去掉lab前缀，解压目录即Go模块根。工程纯标准库，无go.sum。

官方context/net/http/runtime/pprof/os/signal固定Go1.24.3文档，以及Go发布说明、内存模型、race、pipelines和diagnostics页面已实际下载、核读。公开索引book/sources.json保存URL、读取日期与缓存SHA256；原始HTML缓存未提交。未声称生产事故、集群容量或未运行的内核实验。

## 实际Go实验

本机：`go version go1.24.3 darwin/arm64`，Apple M4，CGO=1。最终`go test -race -v ./...`通过，cmd/service 3.075s、internal/service 2.009s；go vet退出0。故障组race重复30轮8.224s；SIGTERM组race重复10轮17.458s。精确命令、原始摘要和profile SHA256见lab/EXPERIMENTS.md与附录B。

Linux CI：`go version go1.24.3 linux/amd64`，Ubuntu24.04，CGO=1。实际结果：

| 验证 | 结果 |
| --- | --- |
| vet、完整race | cmd/service 2.618s；internal/service 1.569s |
| 有界故障race重复30轮 | 7.951s，PASS |
| 真实SIGTERM子进程race重复10轮 | 17.075s，PASS |
| 三次benchmark | 实际运行，保留各次波动，不推导生产容量 |
| CPU/heap profile | 实际生成，go tool pprof成功解析CPU与alloc_space |
| 解压ZIP独立race | cmd/service 2.617s；internal/service 1.568s |

故障断言覆盖：占满worker和队列后及时拒绝；并发提交/关闭；调用者取消不越过已接纳任务所有权；任务截止及取消后active归零；真实TCP未读Body导致连接耗尽、请求截止、关闭后恢复；阻塞读体取消传到服务端并恢复连接槽；真实慢客户端不读256MiB，80ms写截止使handler返回；真实子进程发送SIGTERM，分别验证150ms在途请求与运行/排队任务排空，或80ms预算下5s请求及任务取消、handler/worker实际释放和非零退出。

关闭先等待HTTP，再排空工作池，共享grace预算；预算不足时额外最多1秒合作清理。Go不能强杀忽略取消的Work。本工程202仅表示进程内接纳，任务不持久化；没有业务认证、URL出站策略或跨进程幂等。completed包含错误和取消终结，并非业务成功计数。

## 格式与公网验收

本地与Linux CI均运行build、verify及EPUBCheck5.4.0。23篇源稿、EPUB与网页正文逐章完全一致；元数据、目录、链接、搜索和代码ZIP哈希均通过。EPUBCheck：0 fatals / 0 errors / 0 warnings / 0 infos。

本地浏览器与公网浏览器均使用Playwright1.64.1检查：深色系统下默认浅色、低饱和深色、aias-reader-theme跨标签同步、字号持久化、搜索有/无结果、移动端原生目录、全部23篇320/390px无溢出、无JavaScript阅读/翻页、存储禁用、全部下载以及无JS异常。公网命令：

```sh
PLAYWRIGHT_MODULE=/Volumes/data/aias-homepage/node_modules/playwright BOOK_URL=https://aias00.github.io/go-service-engineering-book/ node scripts/browser-check.cjs
```

公网额外逐篇请求23篇实际HTML，解析article后与源稿独立Pandoc渲染文本归一比较，全部相等；公开build-info的manuscript_sha256与源稿一致。EPUB、离线HTML、book.md和lab.zip分别从公开Pages重新下载，并与成功CI artifact逐字节比较，全部相等。Release EPUB从公开下载URL重新获取，也与同一CI文件逐字节相等。

| 最终文件 | SHA256 |
| --- | --- |
| EPUB（CI = Pages = Release） | `365d394bb4e169fd83e6a63d880fa688d17b31835e5c442075d0e73140e69c32` |
| 完整Markdown/合并书稿 | `24ef30c3fa848e37e6c8623c5f782a294d9ae7efe881bddf147bfb155601fffa` |
| 离线HTML | `76d65e253d8868e1a0c8a56a2100a373ffd5f4ff97d671bc74f053814ce48d85` |
| 独立实验ZIP | `241972a2ca80f467e20cc2d7dd4bb51172f33b1e641ab44bcfcd56990732525d` |

最终EPUB为127759字节。CI的epubcheck.txt也记录相同EPUB SHA256。后续修复应发布补丁版本，保留v1.0.0原附件，不覆盖旧版证据。
