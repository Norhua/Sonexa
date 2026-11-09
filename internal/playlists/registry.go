package playlists

import (
	"fmt"
	"reflect"

	"github.com/norhua/sonexa/internal/provider"
)

type Source struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Registry struct {
	entries []Provider
	byID    map[provider.ProviderID]Provider
}

func NewRegistry(entries ...Provider) (*Registry, error) {
	r := &Registry{byID: map[provider.ProviderID]Provider{}}
	for _, p := range entries {
		if p == nil || (reflect.ValueOf(p).Kind() == reflect.Pointer && reflect.ValueOf(p).IsNil()) || !identity(string(p.ID())) {
			return nil, Failure(Input, "歌单来源缺少身份")
		}
		if _, exists := r.byID[p.ID()]; exists {
			return nil, fmt.Errorf("重复歌单来源 %q", p.ID())
		}
		r.entries = append(r.entries, p)
		r.byID[p.ID()] = p
	}
	return r, nil
}

func (r *Registry) Get(id provider.ProviderID) (Provider, error) {
	if r != nil {
		if p, ok := r.byID[id]; ok {
			return p, nil
		}
	}
	return nil, Failure(Input, "来源未登记或未提供歌单能力")
}

func (r *Registry) List() []Source {
	out := []Source{}
	if r != nil {
		for _, p := range r.entries {
			a := p.PlaylistAvailability()
			out = append(out, Source{ID: string(p.ID()), Name: p.DisplayName(), Available: a.Available, Reason: a.Reason})
		}
	}
	return out
}
