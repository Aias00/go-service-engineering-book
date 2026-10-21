《Go 服务工程：并发、资源管理与故障诊断》v1.0.1。

修正第2、8、16章的生命周期观察方法，并同步澄清第1、3、15章：`/stats`的active只统计Pool接纳的后台任务，不能用于证明`/work`或`/stream` handler已经退出。练习改用request finished/stream finished日志定位阶段，以及ServeHTTP返回后的finished通知或DrainRequests确认handler结束。

本次真实子进程复核：五秒/work请求在客户端50ms预算下以curl退出码28结束，随即记录request finished/context canceled，再SIGTERM正常退出。实验工程接口与实现未变化。

18章正文、前言与4个附录仍完整保留；附件直接来自新一轮成功Linux CI，重跑race、故障30轮、真实SIGTERM10轮、基准/profile、独立ZIP、正文一致性、EPUBCheck及浏览器。v1.0.0保留原附件。最终哈希及公网证据见仓库DELIVERY.md。
