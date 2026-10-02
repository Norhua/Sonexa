package app

import (
	"encoding/base64"
	"fmt"
	"github.com/norhua/sonexa/internal/covers"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
)

type EditCoverDraft struct {
	Token      string `json:"token"`
	PreviewURL string `json:"previewUrl"`
}

func (a *App) PickEditCover() (*EditCoverDraft, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if a.coverSvc == nil || a.coverSvc.Library() == nil {
		return nil, fmt.Errorf("封面服务未就绪")
	}
	pick := a.pickCoverFile
	if pick == nil {
		pick = wruntime.OpenFileDialog
	}
	path, err := pick(a.ctx, wruntime.OpenDialogOptions{Title: "选择专辑封面", Filters: []wruntime.FileFilter{{DisplayName: "封面图片", Pattern: "*.jpg;*.jpeg;*.png;*.webp"}}})
	if err != nil || path == "" {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("封面必须是普通图片文件")
	}
	data, err := covers.ReadImageBytes(file)
	if err != nil {
		return nil, err
	}
	draft, err := a.coverSvc.Library().StageArtwork(data, 0)
	if err != nil {
		return nil, err
	}
	return &EditCoverDraft{Token: draft.Token, PreviewURL: "data:image/" + draft.Format + ";base64," + base64.StdEncoding.EncodeToString(data)}, nil
}
func (a *App) DiscardEditCover(token string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.coverSvc == nil || a.coverSvc.Library() == nil {
		return nil
	}
	return a.coverSvc.Library().DiscardArtwork(token)
}

func (a *App) StageEditUpstreamCover(previewID string) (*EditCoverDraft, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	target, err := service.EditArtworkTarget(previewID)
	if err != nil || target == nil {
		return nil, err
	}
	if a.coverSvc == nil || a.coverSvc.Library() == nil {
		return nil, fmt.Errorf("封面服务未就绪")
	}
	resolved, err := a.coverSvc.Resolve(a.ctx, covers.Target{Provider: string(target.Provider), Ref: target.Ref, Preview: true}, covers.Detail)
	if err != nil {
		return nil, err
	}
	if resolved == nil || resolved.File == nil || len(resolved.Data) == 0 {
		return nil, fmt.Errorf("上游封面暂时不可用，请重试")
	}
	draft, err := a.coverSvc.Library().StageArtwork(resolved.Data, resolved.File.VariantID)
	if err != nil {
		return nil, err
	}
	return &EditCoverDraft{Token: draft.Token, PreviewURL: "data:image/" + draft.Format + ";base64," + base64.StdEncoding.EncodeToString(resolved.Data)}, nil
}
