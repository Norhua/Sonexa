package main

import "time"

// formatTime 把可空时间渲染为 RFC3339 串。
//
// 绑定层一律以字符串导出时间：Wails 的 TS 模型生成器不认识 time.Time，
// 遇到它会打印 `Not found: time.Time` 并把字段生成为 any——
// 前端本来就是 new Date(...) 消费，字符串既不丢信息，类型也更准确。
func formatTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
