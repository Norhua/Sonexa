package sources

import (
	"context"
	"fmt"
	"strings"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

// Query 是封面业务输入；Release 可选，不要求调用方持有数据库发行。
type Query struct {
	Title   string
	Artist  string
	Album   string
	Country string
	Limit   int
	Force   bool
	Release *domain.ReleaseIdentity
}

// Provider 只要求封面查询能力，协议及原生身份解释由具体上游负责。
type Provider interface {
	provider.Upstream
	SearchCovers(context.Context, Query) ([]Result, error)
}

// Entry 声明业务显示名称和地区需求，避免与元数据或歌词的文案耦合。
type Entry struct {
	Provider     Provider
	Name         string
	NeedsCountry bool
}

// Result 恰好包含一种形式：新目录输入，或共享目录服务已经登记的引用。
// 已登记目录只读取，禁止再次保存而覆盖目录版本、错误记忆与文件关联。
// 消费结果时必须 Validate；零值及同时设置两种形式均为错误。
type Result struct {
	Input    *domain.CoverCatalogInput
	Existing *domain.CoverCatalogKey
}

func (r Result) Validate() error {
	if (r.Input == nil) == (r.Existing == nil) {
		return fmt.Errorf("封面结果必须且只能包含新目录或已登记目录引用")
	}
	key := r.Key()
	if strings.TrimSpace(key.Source) == "" || strings.TrimSpace(key.ObjectType) == "" || strings.TrimSpace(key.SourceRef) == "" {
		return fmt.Errorf("封面结果缺少目录身份")
	}
	return nil
}

func (r Result) Key() domain.CoverCatalogKey {
	if r.Input != nil {
		return r.Input.Key
	}
	if r.Existing != nil {
		return *r.Existing
	}
	return domain.CoverCatalogKey{}
}
