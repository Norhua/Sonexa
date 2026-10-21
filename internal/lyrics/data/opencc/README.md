# OpenCC 繁简字符字典

- 上游：<https://github.com/BYVoid/OpenCC>
- 版本：`ver.1.1.9`，提交 `556ed22496d650bd0b13b6c163be9814637970ae`
- 原文件：`data/dictionary/TSCharacters.txt`，此目录保留未修改副本。
- SHA-256：`6b5a0a799bea2bb22c001f635eaa3fc2904310f0c08addbff275477a80ecf09a`
- 许可证：Apache-2.0，完整许可与作者列表分别见 `LICENSE`、`AUTHORS`（仅移除文件末尾额外空行，正文未改）。
- 下载：`https://raw.githubusercontent.com/BYVoid/OpenCC/556ed22496d650bd0b13b6c163be9814637970ae/data/dictionary/TSCharacters.txt`

Sonexa 独立实现的 `../../credit.go` 只读取唯一单字到单字映射，用于歌词搜索艺术家逐字等价比较；多候选映射不选默认项，不引入词汇替换、日文新旧字体或转换后的模糊匹配。显示名称、上游查询、存储、歌词正文和元数据匹配均不改写。

字典静态嵌入程序，不读取用户机器的 OpenCC 安装、不请求网络。未引入 OpenCC 或其他 Go 转换器的运行时代码/传递依赖。分发含该字典的程序时需随附本目录的许可证与归属说明。
