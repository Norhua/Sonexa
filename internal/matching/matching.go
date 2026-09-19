// Package matching 实现文本标准化、括号语义分类、查询变体生成、CV 解析、
// 置信度评分、release 优选阶梯与候选合并判定。
//
// 本包不依赖任何 provider 的具体实现，只依赖接口与候选结构，
// 使置信度算法可对着固定的金样例离线测试。
package matching
