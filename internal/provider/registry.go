package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Registry 登记全部可用上游。
//
// 界面中所有「选择上游」的位置都从 List() 动态渲染，
// 禁止硬编码 provider 名称或数量——新增一个上游应当零页面代码改动。
type Registry struct {
	mu        sync.RWMutex
	providers map[ProviderID]Provider
	// priority 是上游的展示优先级顺序，来自设置。
	priority []ProviderID
}

// NewRegistry 建立一个空注册表。
func NewRegistry() *Registry {
	return &Registry{providers: map[ProviderID]Provider{}}
}

// Register 登记一个上游。同一标识重复登记时后者覆盖前者。
func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.ID()] = p
}

// Get 按标识取用上游；未登记时返回错误。
func (r *Registry) Get(id ProviderID) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[id]
	if !ok {
		return nil, fmt.Errorf("上游 %q 未登记", id)
	}
	return p, nil
}

// List 返回全部已登记的上游，按设置的优先级顺序排列；
// 未在优先级列表中的按标识字典序排在其后（保证顺序稳定）。
func (r *Registry) List() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rank := map[ProviderID]int{}
	for i, id := range r.priority {
		rank[id] = i
	}

	out := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, oki := rank[out[i].ID()]
		rj, okj := rank[out[j].ID()]
		switch {
		case oki && okj:
			return ri < rj
		case oki:
			return true
		case okj:
			return false
		default:
			return out[i].ID() < out[j].ID()
		}
	})
	return out
}

// SetPriority 设置上游的展示优先级顺序。
func (r *Registry) SetPriority(order []ProviderID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.priority = append([]ProviderID{}, order...)
}

// Len 返回已登记的上游数量。
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.providers)
}

// CapabilitiesOf 返回一组上游的能力并集，供 UI 的列显示策略使用。
//
// 列策略以「当次结果涉及的上游集合」为准：集合中所有上游都不支持某列时
// 隐藏该列；仅部分支持时显示该列并在不支持的行填占位短横线。
func (r *Registry) CapabilitiesOf(ids []ProviderID) Capabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var union Capabilities
	for _, id := range ids {
		if p, ok := r.providers[id]; ok {
			union |= p.Capabilities()
		}
	}
	return union
}
