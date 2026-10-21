package lyrics

import (
	"context"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/provider"
)

// Provider 是歌词业务消费的能力，不要求元数据或封面方法。
// 适配器在返回前完成原生解析、选轨、通道合并和序列化；业务只读完整文档。
type Provider interface {
	provider.Upstream
	SearchLyrics(context.Context, Query) ([]Candidate, error)
	FetchLyrics(context.Context, Candidate) (*lyricdoc.Document, error)
}
