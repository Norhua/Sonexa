package lyrics

import (
	"fmt"
	"strings"

	"github.com/norhua/sonexa/internal/provider"
)

// Entry 保留歌词业务显示名；为空时使用上游显示名。
type Entry struct {
	Provider Provider
	Name     string
}

// Registry 是构造后只读的歌词能力集合，顺序就是登记顺序。
// 列表、校验与执行共用此集合，不回退到元数据注册表。
type Registry struct {
	entries []Entry
	byID    map[provider.ProviderID]Provider
}

func NewRegistry(entries ...Entry) (*Registry, error) {
	r := &Registry{byID: make(map[provider.ProviderID]Provider)}
	for _, entry := range entries {
		if entry.Provider == nil || strings.TrimSpace(string(entry.Provider.ID())) == "" {
			return nil, fmt.Errorf("歌词来源缺少身份")
		}
		id := entry.Provider.ID()
		if _, exists := r.byID[id]; exists {
			return nil, fmt.Errorf("重复歌词来源 %q", id)
		}
		if entry.Name == "" {
			entry.Name = entry.Provider.DisplayName()
		}
		r.entries = append(r.entries, entry)
		r.byID[id] = entry.Provider
	}
	return r, nil
}

func (r *Registry) List() []Source {
	out := []Source{}
	if r != nil {
		for _, entry := range r.entries {
			out = append(out, Source{ID: string(entry.Provider.ID()), Name: entry.Name})
		}
	}
	return out
}

func (r *Registry) Get(id provider.ProviderID) (Provider, error) {
	if r != nil {
		if p, ok := r.byID[id]; ok {
			return p, nil
		}
	}
	return nil, fmt.Errorf("歌词来源 %q 未登记或未提供歌词能力", id)
}
