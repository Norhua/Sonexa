# 测试数据约定

全部涉及外部数据的解析器都配有金样例（Golden Tests）：编码探测、分隔符探测、
文本标准化、括号分类、CV 解析、置信度打分。

## 目录

```
testdata/golden/text/       文本标准化、括号分类、CV 解析的输入与期望
testdata/golden/encoding/   各编码的样例文件（GB18030 / Shift_JIS / Big5 / UTF-16）
testdata/golden/delimiter/  分隔符与表头探测的样例文件
testdata/golden/scoring/    置信度打分的候选与期望分值
testdata/golden/responses/  上游原始响应，供解析器断网重放
```

## 约定

- 金样例以表驱动测试消费，用例名即输入的简短描述。
- `responses/` 下的响应即 `provider_cache` 中落盘的原始内容，
  解析器测试不出网，直接读这些文件。
- 修改任一解析规则时，金样例负责检出对既有形态的行为回归。
