package covers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

type Usage string

const (
	Thumbnail Usage = "thumbnail"
	Detail    Usage = "detail"
)

// Target 以本地歌集或明确的上游发行定位。Ref 不再接受图片 URL。
type Target struct {
	ReleaseID int64  `json:"releaseId"`
	Provider  string `json:"provider"`
	Ref       string `json:"ref"`
	GroupRef  string `json:"groupRef"`
}

func (t Target) Key(usage Usage, revision int64) string {
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
	} else if target.Provider != "" && target.Ref != "" {
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

func (s *Service) localFiles(target Target) ([]store.CoverFile, error) {
	if target.ReleaseID > 0 {
		if err := s.library.ReuseReleasePreviews(target.ReleaseID); err != nil {
			return nil, err
		}
		return s.library.store.ListReleaseCoverFiles(target.ReleaseID)
	}
	if target.Provider != "musicbrainz" || target.Ref == "" {
		return nil, nil
	}
	catalog, err := s.library.store.GetCoverCatalog(store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: target.Ref})
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
	files, err := s.localFiles(target)
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
	if target.Provider != "musicbrainz" || !validMBID(target.Ref) {
		if fallback != nil {
			return fallback, nil
		}
		return &Resolved{Outcome: OutcomeEmpty}, nil
	}
	catalog, err := s.CAACatalog(ctx, "release", target.Ref, false)
	if err == nil && catalog.State == "empty" && validMBID(target.GroupRef) {
		catalog, err = s.CAACatalog(ctx, "release-group", target.GroupRef, false)
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

func validMBID(ref string) bool {
	id, err := uuid.Parse(ref)
	return err == nil && id.String() == strings.ToLower(ref)
}

func (s *Service) CAACatalog(ctx context.Context, kind, ref string, force bool) (*store.CoverCatalog, error) {
	ctx, done, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !validMBID(ref) || (kind != "release" && kind != "release-group") {
		return nil, fmt.Errorf("无效 MusicBrainz 发行身份")
	}
	key := store.CoverCatalogKey{Source: "caa", ObjectType: kind, SourceRef: ref}
	cached, err := s.library.store.GetCoverCatalog(key)
	if err != nil {
		return nil, err
	}
	if !force && cached != nil {
		if cached.State == "found" || cached.State == "empty" {
			return cached, nil
		}
		if cached.RetryAfter != nil && time.Now().Before(*cached.RetryAfter) {
			return nil, fmt.Errorf("CAA 暂时不可用: %s", cached.LastError)
		}
	}
	value, err := s.pool.Shared(ctx, "catalog:"+kind+":"+ref, func(ctx context.Context) (any, error) {
		// 等待同键任务期间可能已经有持久化目录；仅显式刷新绕过。
		if !force {
			if got, err := s.library.store.GetCoverCatalog(key); err != nil {
				return nil, err
			} else if got != nil && (got.State == "found" || got.State == "empty") {
				return got, nil
			}
		}
		body, err := s.pool.fetchOne(ctx, musicbrainz.CoverArtArchiveBaseURL+"/"+kind+"/"+ref)
		var images []store.CoverImage
		actualRelease := ""
		if errors.Is(err, ErrNotFound) {
			err = nil
		} else if err == nil {
			var index *musicbrainz.CoverIndex
			index, err = musicbrainz.ParseCoverCatalog(body)
			if err == nil {
				actualRelease = index.Release
				for _, image := range index.Images {
					entry := store.CoverImage{SourceImageID: image.ID, Types: image.Types, PrimaryType: image.Type, Front: image.Front, Back: image.Back, OriginalURL: image.Original}
					for _, v := range image.Variants {
						entry.Variants = append(entry.Variants, store.CoverVariant{Spec: v.Spec, URL: v.URL})
					}
					images = append(images, entry)
				}
			}
		}
		if err != nil {
			var disabled *OutboundError
			if ctx.Err() == nil && !errors.As(err, &disabled) {
				_ = s.library.store.RecordCoverCatalogError(key, err.Error(), time.Now().Add(time.Minute))
			}
			return nil, err
		}
		id, err := s.library.store.SaveCoverCatalog(key, images)
		if err != nil {
			return nil, err
		}
		if err := s.library.store.SetCoverCatalogRelease(id, actualRelease); err != nil {
			return nil, err
		}
		return s.library.store.GetCoverCatalog(key)
	})
	if err != nil {
		return nil, err
	}
	return value.(*store.CoverCatalog), nil
}

func (s *Service) downloadCatalog(ctx context.Context, target Target, catalog *store.CoverCatalog, usage Usage) (resolved *Resolved, failure error) {
	images := append([]store.CoverImage(nil), catalog.Images...)
	sort.SliceStable(images, func(i, j int) bool { return images[i].Front && !images[j].Front })
	specs := []string{"250", "500", "1200", "original"}
	if usage == Detail {
		opts := []string{"1200", "original"}
		specs = opts
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
	if err := validImageURL(variant.URL); err != nil {
		return nil, err
	}
	value, err := s.pool.Shared(ctx, fmt.Sprintf("image:%d", variant.ID), func(ctx context.Context) (any, error) {
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
		data, err := s.pool.fetchURLs(ctx, caaImageLadder(variant.URL), nil)
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

var caaImagePath = regexp.MustCompile(`^/release/([0-9a-f-]{36})/([0-9]+)(?:-(250|500|1200))?\.([a-z]+)$`)

// caaImageLadder 把 CAA 图片地址改写为它最终 307 到的 archive.org 地址，省去一次
// coverartarchive.org 往返（每跳都需独立 TLS 握手，慢网络下单跳可达数秒）。
// 直连失败时退回规范地址，archive.org 布局变化只会让它变慢而不会取不到图。
func caaImageLadder(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "coverartarchive.org") {
		return []string{raw}
	}
	m := caaImagePath.FindStringSubmatch(u.Path)
	if m == nil {
		return []string{raw}
	}
	name := "mbid-" + m[1] + "-" + m[2]
	if m[3] != "" {
		name += "_thumb" + m[3]
	}
	direct := "https://archive.org/download/mbid-" + m[1] + "/" + name + "." + m[4]
	return []string{direct, raw}
}

func validImageURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("封面地址必须使用可信 HTTPS 来源")
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"coverartarchive.org", "archive.org", "mzstatic.com", "y.qq.com", "music.126.net"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return nil
		}
	}
	return fmt.Errorf("封面地址不是已支持来源")
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
