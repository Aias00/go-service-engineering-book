# 附录A：官方来源与阅读边界

本书于2026年10月21日实际下载并阅读以下官方材料。标准库API链接固定Go 1.24.3；Go博客和语言规范是持续维护文档，不能把页面当前内容自动当作全部版本的行为。原始网页仅存本地研究缓存，不随仓库或电子书再分发；来源索引保存读取日期、URL与缓存SHA256。

| 来源 | 对应问题 | 使用范围 |
| --- | --- | --- |
| [Go 1.24发布说明](https://go.dev/doc/go1.24) | 版本基线、运行时与标准库变化 | 实验基线是实际go1.24.3，不宣称最新版 |
| [context@go1.24.3](https://pkg.go.dev/context@go1.24.3) | CancelFunc、截止、Cause、WithoutCancel | 第2、3、6、10章的取消协议与调用者责任 |
| [net/http@go1.24.3](https://pkg.go.dev/net/http@go1.24.3) | Body、Transport、Shutdown、ResponseController | 第7至11章协议和生命周期 |
| [os/signal@go1.24.3](https://pkg.go.dev/os/signal@go1.24.3) | NotifyContext与stop | 第11章信号处理范围 |
| [Go内存模型](https://go.dev/ref/mem) | 同步关系与可见性 | 第4章，不把调度或Sleep当作同步 |
| [Data Race Detector](https://go.dev/doc/articles/race_detector) | race的使用与动态检测边界 | 第12、13章，只覆盖实际运行路径 |
| [Go Concurrency Patterns: Pipelines and cancellation](https://go.dev/blog/pipelines) | channel关闭、上游退出与下游提前离开 | 第2至4章生命周期设计 |
| [Diagnostics](https://go.dev/doc/diagnostics) | profiling、tracing、运行时信息的用途 | 第14至16章工具选择 |
| [runtime/pprof@go1.24.3](https://pkg.go.dev/runtime/pprof@go1.24.3) | heap、allocs、block与mutex采样 | 第14、15章，不把alloc_space当存活堆 |

阅读标准库文档时，本书特别核对了五个容易误用的地方。CancelFunc需要调用以释放关联资源；Client.Do在非二百状态时也可能返回无错误响应；Body读取与关闭影响连接复用；MaxConnsPerHost包含建立中、活跃和空闲连接并可能导致等待；Server.Shutdown不等待任意后台任务或hijack连接。

文档给出机制契约，实验验证本工程组合机制后的行为。二者不能互相替代。一个API允许并发使用，不代表调用者修改的共享对象也安全；一个关闭函数返回，也不代表全部业务goroutine已结束。书中断言以明确工作负载和测试边界为准。

本书没有引用虚构生产故障，没有声称测量内核TCP实现，也没有运行跨集群容量实验。HTTP/2、TLS代理、真实数据库、外部幂等系统与容器编排的建议属于扩展设计，需要在实际部署路径重新验证。读者复用代码时，应先识别这些依赖边界。
