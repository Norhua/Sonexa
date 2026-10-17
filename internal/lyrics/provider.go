package lyrics

import (
	"context"

	"github.com/norhua/sonexa/internal/provider"
)

// Provider 是歌词业务消费的能力，不要求元数据或封面方法。
// 适配器解释原生身份及协议；业务服务负责校验、评分、去重与最终文档。
type Provider interface {
	provider.Upstream
	SearchLyrics(context.Context, Query) ([]Candidate, error)
	FetchLyrics(context.Context, Candidate) (*Payload, error)
}

// Payload 只在后端能力边界流转，不作为 Wails DTO。
// Text 用于已解码的单文本；多通道来源使用 Original/Translation/Romanization。
// 通道错误和缺失必须分开保留，由 Service 统一合并并生成 Document。
type Payload struct {
	Text         string
	Original     *Channel
	Translation  *Channel
	Romanization *Channel
	WordTiming   string
	Warnings     []string
}
