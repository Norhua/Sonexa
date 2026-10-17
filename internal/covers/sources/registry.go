package sources

import (
	"fmt"
	"strings"

	"github.com/norhua/sonexa/internal/provider"
)

// Registry 是构造后只读的封面能力集合，不改变元数据 Registry 的覆盖语义。
// 顺序和显示名由装配显式声明，列表、校验及分发读取同一登记信息。
type Registry struct {
	entries []Entry
	byID    map[provider.ProviderID]Provider
}

func NewRegistry(entries ...Entry) (*Registry, error) {
	r := &Registry{byID: make(map[provider.ProviderID]Provider)}
	for _, entry := range entries {
		if entry.Provider == nil || strings.TrimSpace(string(entry.Provider.ID())) == "" {
			return nil, fmt.Errorf("封面来源缺少身份")
		}
		id := entry.Provider.ID()
		if _, exists := r.byID[id]; exists {
			return nil, fmt.Errorf("重复封面来源 %q", id)
		}
		if entry.Name == "" {
			entry.Name = entry.Provider.DisplayName()
		}
		r.entries = append(r.entries, entry)
		r.byID[id] = entry.Provider
	}
	return r, nil
}

func (r *Registry) List() []Info {
	out := []Info{}
	if r != nil {
		for _, entry := range r.entries {
			out = append(out, Info{ID: string(entry.Provider.ID()), Name: entry.Name, NeedsCountry: entry.NeedsCountry})
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
	return nil, fmt.Errorf("封面来源 %q 未登记或未提供封面能力", id)
}
