package covers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/norhua/sonexa/internal/store"
)

type Usage string

const (
	Thumbnail Usage = "thumbnail"
	Detail    Usage = "detail"
)

// Target 以本地歌集或明确的上游发行定位。Ref 不再接受图片 URL。
type Target struct {
	Preview   bool   `json:"-"`
	ReleaseID int64  `json:"releaseId"`
	Provider  string `json:"provider"`
	Ref       string `json:"ref"`
	GroupRef  string `json:"groupRef"`
}

func (t Target) Key(usage Usage, revision int64) string {
	if t.Preview {
		return fmt.Sprintf("preview:%s|%s|%s|%s|%d", t.Provider, t.Ref, t.GroupRef, usage, revision)
	}
	if t.ReleaseID > 0 {
		return fmt.Sprintf("release:%d|%s|%d", t.ReleaseID, usage, revision)
	}
	return fmt.Sprintf("%d|%s|%s|%s|%s|%d", t.ReleaseID, t.Provider, t.Ref, t.GroupRef, usage, revision)
}

type Resolved struct {
	File    *store.CoverFile
	Data    []byte
	Outcome Outcome
	Warning string
}

type OutboundError struct{ Reason string }

func (e *OutboundError) Error() string { return e.Reason }

func (s *Service) resolveTarget(target Target) (Target, error) {
	if target.ReleaseID > 0 {
		release, err := s.library.store.GetRelease(target.ReleaseID)
		if err != nil {
			return target, err
		}
		if release == nil {
			return target, fmt.Errorf("歌集不存在")
		}
		target.Provider, target.Ref, target.GroupRef = "", "", ""
		if release.Provider != nil {
			target.Provider = *release.Provider
		}
		if release.ProviderRef != nil {
			target.Ref = *release.ProviderRef
		}
		if release.ReleaseGroupRef != nil {
			target.GroupRef = *release.ReleaseGroupRef
		}
	} else if !target.Preview && target.Provider != "" && target.Ref != "" {
		release, err := s.library.store.GetReleaseByRef(target.Provider, target.Ref)
		if err != nil {
			return target, err
		}
		if release != nil {
			target.ReleaseID = release.ID
			return s.resolveTarget(target)
		}
	}
	return target, nil
}

func (s *Service) localFiles(ctx context.Context, target Target) ([]store.CoverFile, error) {
	if target.ReleaseID > 0 {
		if err := s.library.ReuseReleasePreviews(target.ReleaseID); err != nil {
			return nil, err
		}
		return s.library.store.ListReleaseCoverFiles(target.ReleaseID)
	}
	if target.Ref == "" {
		return nil, nil
	}
	policy, ok := artworkForProviderIn(ctx, target.Provider)
	if !ok {
		return nil, nil
	}
	key := store.CoverCatalogKey{Source: policy.Source, ObjectType: policy.ObjectType, SourceRef: target.Ref}

	catalog, err := s.library.store.GetCoverCatalog(key)
	if err != nil || catalog == nil {
		return nil, err
	}
	return s.library.store.AutoPreviewFiles(catalog.ID)
}

func (s *Service) chooseFile(files []store.CoverFile, usage Usage, threshold bool) *Resolved {
	sort.SliceStable(files, func(i, j int) bool {
		a, b := max(files[i].Width, files[i].Height), max(files[j].Width, files[j].Height)
		if a == b {
			return files[i].VariantID < files[j].VariantID
		}
		if usage == Detail {
			return a > b
		}
		return a < b
	})
	for _, file := range files {
		size := max(file.Width, file.Height)
		if !file.Available || (threshold && ((usage == Detail && size <= 800) || (usage == Thumbnail && size >= 600))) {
			continue
		}
		data, err := s.library.Read(file)
		if err == nil {
			return &Resolved{File: &file, Data: data, Outcome: OutcomeOK}
		}
	}
	return nil
}

func (s *Service) Resolve(ctx context.Context, requested Target, usage Usage) (*Resolved, error) {
	ctx, done, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if s.library == nil {
		return nil, fmt.Errorf("专辑封面服务未装配")
	}
	if usage != Thumbnail && usage != Detail {
		return nil, fmt.Errorf("未知封面用途")
	}
	target, err := s.resolveTarget(requested)
	if err != nil {
		return nil, err
	}
	var selection *store.CoverSelection
	if target.ReleaseID > 0 {
		if selection, err = s.library.store.GetCoverSelection(target.ReleaseID); err != nil {
			return nil, err
		}
		// 用户清除过封面：不复用预览、不自动获取，直到重新采用。
		if selection != nil && selection.Cleared {
			return &Resolved{Outcome: OutcomeEmpty}, nil
		}
	}
	files, err := s.localFiles(ctx, target)
	if err != nil {
		return nil, err
	}
	if target.ReleaseID > 0 {
		if selection != nil {
			selected := []store.CoverFile{}
			for _, file := range files {
				variant, err := s.library.store.GetCoverVariant(file.VariantID)
				if err != nil {
					return nil, err
				}
				if variant != nil && variant.ImageID == selection.ImageID {
					selected = append(selected, file)
				}
			}
			if got := s.chooseFile(selected, usage, false); got != nil {
				return got, nil
			}
			return nil, fmt.Errorf("已采用封面文件丢失，请重新选择图片")
		}
	}
	if got := s.chooseFile(files, usage, true); got != nil {
		return got, nil
	}
	fallback := s.chooseFile(files, usage, false)
	var catalog *store.CoverCatalog
	policy, ok := artworkForProviderIn(ctx, target.Provider)
	if !ok || policy.Validate(policy.ObjectType, target.Ref) != nil {
		if fallback != nil {
			return fallback, nil
		}
		return &Resolved{Outcome: OutcomeEmpty}, nil
	}
	catalog, err = s.Catalog(ctx, store.CoverCatalogKey{Source: policy.Source, ObjectType: policy.ObjectType, SourceRef: target.Ref}, false)
	if err == nil && catalog.State == "empty" && policy.GroupFallback && policy.Validate("release-group", target.GroupRef) == nil {
		catalog, err = s.Catalog(ctx, store.CoverCatalogKey{Source: policy.Source, ObjectType: "release-group", SourceRef: target.GroupRef}, false)
	}

	if err == nil && catalog.State == "found" {
		var got *Resolved
		got, err = s.downloadCatalog(ctx, target, catalog, usage)
		if err == nil {
			if got != nil {
				return got, nil
			}
		}
	}
	if fallback != nil {
		if err != nil {
			fallback.Warning = err.Error()
		}
		return fallback, nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return &Resolved{Outcome: OutcomeEmpty}, nil
}

func (s *Service) downloadCatalog(ctx context.Context, target Target, catalog *store.CoverCatalog, usage Usage) (resolved *Resolved, failure error) {
	images := append([]store.CoverImage(nil), catalog.Images...)
	sort.SliceStable(images, func(i, j int) bool { return images[i].Front && !images[j].Front })
	specs := []string{"original"}
	if policy, ok := artworkIn(ctx, catalog.Source); ok && policy.Specs != nil {
		specs = policy.Specs(usage == Detail)
	}

	for _, image := range images {
		for _, spec := range specs {
			for _, variant := range image.Variants {
				if variant.Spec != spec || variant.State == "unavailable" {
					continue
				}
				data, err := s.DownloadVariant(ctx, variant)
				if err != nil {
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					if !errors.Is(err, ErrNotFound) {
						failure = err
					}
					continue
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				file, _, err := s.library.SaveVariant(target.ReleaseID, variant.ID, data, false, 0)
				if err != nil {
					return nil, err
				}
				if target.ReleaseID == 0 {
					if err := s.library.store.MarkAutoCoverPreview(variant.ID); err != nil {
						return nil, err
					}
				}
				return &Resolved{File: file, Data: data, Outcome: OutcomeOK}, nil
			}
		}
	}
	if failure != nil {
		return nil, failure
	}
	return nil, ErrNotFound
}

// DownloadVariant 不接受前端任意 URL，规格必须来自已登记目录。
func (s *Service) DownloadVariant(ctx context.Context, variant store.CoverVariant) ([]byte, error) {
	ctx, done, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	source, err := s.library.store.CoverSourceForVariant(variant.ID)
	if err != nil {
		return nil, err
	}
	ctx = imageContext(ctx, source)
	value, err := s.pool.Shared(ctx, fmt.Sprintf("image:%d:%s", variant.ID, imagePolicy(ctx).Key()), func(ctx context.Context) (any, error) {
		// 先读同规格的临时预览，避免页面预览后采用再下一遍。
		files, err := s.library.store.PreviewFilesForVariant(variant.ID)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.Available {
				if data, err := s.library.Read(file); err == nil {
					return data, nil
				}
			}
		}
		if err := imagePolicy(ctx).CheckURL(variant.URL); err != nil {
			return nil, err
		}
		data, err := s.pool.fetchURLs(ctx, s.imageURLs(ctx, source, variant.URL), nil)
		if err == nil {
			_, err = InspectImage(data)
		}
		if err != nil {
			var disabled *OutboundError
			if ctx.Err() == nil && !errors.As(err, &disabled) {
				_ = s.library.store.RecordCoverVariantError(variant.ID, errors.Is(err, ErrNotFound), err.Error())
			}
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, _, err := s.library.SaveVariant(0, variant.ID, data, false, 0); err != nil {
			return nil, err
		}
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]byte), nil
}

func revisionFromQuery(q url.Values) int64 {
	revision, _ := strconv.ParseInt(q.Get("revision"), 10, 64)
	return revision
}

func (s *Service) serveLibrary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if raw := q.Get("variantId"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "图片规格标识无效", http.StatusBadRequest)
			return
		}
		variant, err := s.library.store.GetCoverVariant(id)
		if err != nil || variant == nil || !variant.Active {
			http.NotFound(w, r)
			return
		}
		data, err := s.DownloadVariant(r.Context(), *variant)
		if err != nil {
			var disabled *OutboundError
			if errors.As(err, &disabled) {
				http.Error(w, err.Error(), http.StatusForbidden)
				return
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeImage(w, data)
		return
	}
	usage := Usage(q.Get("usage"))
	if usage == "" {
		usage = Thumbnail
	}
	releaseID, _ := strconv.ParseInt(q.Get("releaseId"), 10, 64)
	target := Target{ReleaseID: releaseID, Provider: q.Get("provider"), Ref: q.Get("ref"), GroupRef: q.Get("rg")}
	resolved, err := s.Resolve(r.Context(), target, usage)
	key := target.Key(usage, revisionFromQuery(q))
	if err != nil {
		s.recordOutcome(key, OutcomeError)
		var disabled *OutboundError
		if errors.As(err, &disabled) {
			s.recordOutcome(key, Outcome("disabled"))
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	s.recordOutcome(key, resolved.Outcome)
	if resolved.Outcome == OutcomeEmpty {
		http.NotFound(w, r)
		return
	}
	writeImage(w, resolved.Data)
}
