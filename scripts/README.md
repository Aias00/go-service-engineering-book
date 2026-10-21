# 构建说明

`check-manuscript.py`确认18章及排除代码的正文汉字门槛。`build.py`从book/metadata.json和book/chapters生成网页/EPUB/离线HTML；lab文本源码直接进入代码附录，ZIP去掉lab前缀，以Go模块根布局分发。排除二进制profile、证据和隐藏文件，保留Go源码、go.mod/go.sum（若存在）与嵌入文本资源。

`verify.py`独立比较源、网页、EPUB逐章全文，验证链接、元数据、目录、搜索和ZIP文件哈希。`epubcheck.sh`使用5.4.0并failonwarnings。`browser-check.cjs`检查浅色默认、共享主题/跨标签同步、字号、搜索、全部章节320/390px溢出、无JS及禁用存储。

证据文档DELIVERY/RELEASE_NOTES不进入产品包，也不触发Pages构建；所有实际打包输入含根README和LICENSE都纳入workflow paths。
