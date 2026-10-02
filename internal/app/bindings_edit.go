package app

import "github.com/norhua/sonexa/internal/domain"

// EditSession 只包含离线基线与来源能力；写入统一通过编辑计划。
type EditSession struct {
	Editor    *domain.EditorSession `json:"editor"`
	Providers []ProviderInfo        `json:"providers"`
}

func (a *App) GetEditSession(trackID int64) (*EditSession, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	editor, err := a.store.ReadEditorSession(trackID)
	if err != nil {
		return nil, err
	}
	providers, err := a.ListProviders()
	if err != nil {
		return nil, err
	}
	return &EditSession{Editor: editor, Providers: providers}, nil
}
