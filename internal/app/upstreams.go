package app

import (
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/kugou"
	"github.com/norhua/sonexa/internal/provider/kuwo"
	"github.com/norhua/sonexa/internal/provider/netease"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

// buildLyricsService is shared by production assembly and composition tests.
// Existing sessions retain this immutable service when settings rebuild it.
func buildLyricsService(client *provider.Client, qq *qqmusic.Provider) (*lyrics.Service, error) {
	r, err := lyrics.NewRegistry(
		lyrics.Entry{Provider: netease.New(client), Name: "网易云音乐"},
		lyrics.Entry{Provider: qq, Name: "QQ 音乐"},
		lyrics.Entry{Provider: kugou.New(client), Name: "酷狗音乐"},
		lyrics.Entry{Provider: kuwo.New(client), Name: "酷我音乐"},
	)
	if err != nil {
		return nil, err
	}
	return &lyrics.Service{Sources: r}, nil
}
