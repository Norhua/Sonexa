package library

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
	"time"
)

type storedEditPreview struct {
	snapshot domain.Snapshot
	expires  time.Time
}

func (s *Service) storeEditPreview(snapshot domain.Snapshot) (*domain.EditPreview, error) {
	copy, err := clone(snapshot)
	if err != nil {
		return nil, err
	}
	id, err := token()
	if err != nil {
		return nil, err
	}
	fields, err := domain.EditorFields(copy)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(copy)
	if err != nil {
		return nil, err
	}
	expires := s.clock.Now().Add(PlanTTL)
	s.mu.Lock()
	for key, value := range s.previews {
		if !s.clock.Now().Before(value.expires) {
			delete(s.previews, key)
		}
	}
	s.previews[id] = storedEditPreview{snapshot: copy, expires: expires}
	s.mu.Unlock()
	preview := &domain.EditPreview{ID: id, ExpiresAt: expires.UTC().Format(time.RFC3339Nano), SnapshotJSON: string(raw), Fields: fields}
	preview.TrackStates, err = s.store.EditorTracklistStates(copy.Release)
	if err != nil {
		return nil, err
	}
	if copy.Track != nil && copy.Track.Identity.Ref != "" {
		identity := copy.Track.Identity
		preview.TrackIdentity = &identity
	}
	if copy.Release != nil && copy.Release.Identity.Ref != "" {
		identity := copy.Release.Identity
		preview.ReleaseIdentity = &identity
	}
	return preview, nil
}
func (s *Service) editPreview(id string) (domain.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	preview, ok := s.previews[id]
	if !ok || !s.clock.Now().Before(preview.expires) {
		return domain.Snapshot{}, store.ErrPlanStale
	}
	return clone(preview.snapshot)
}
func (s *Service) PreviewEditCandidate(candidate provider.Candidate) (*domain.EditPreview, error) {
	seed, err := s.CandidateSeed(candidate, 0)
	if err != nil {
		return nil, err
	}
	return s.storeEditPreview(seed.Snapshot)
}

// LoadEditUpstream 只读取来源及网络缓存，不同步正式发行、曲目或归属。
func (s *Service) LoadEditUpstream(ctx context.Context, source, releaseInput, trackInput, region string, force bool) (*domain.EditPreview, error) {
	p, err := s.provider(domain.ProviderID(source))
	if err != nil {
		return nil, err
	}
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}
	codec := p.Refs()
	if codec == nil {
		return nil, provider.ErrUnsupported
	}
	var release, track domain.Identity
	if releaseInput != "" {
		release, err = codec.ParseRelease(releaseInput, region)
		if err != nil {
			return nil, err
		}
	}
	if trackInput != "" {
		track, err = codec.ParseTrack(trackInput, region)
		if err != nil {
			return nil, err
		}
	}
	if release.Ref == "" && track.Ref == "" {
		return nil, fmt.Errorf("请填写歌曲或发行身份")
	}
	exact, err := codec.Target(release.Ref, track.Ref)
	if err != nil {
		return nil, err
	}
	exact.SkipCache = force
	snapshot, err := p.Complete(ctx, exact)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, store.ErrSnapshotTarget
	}
	if track.Ref != "" && (snapshot.Track == nil || snapshot.Track.Identity != track) {
		return nil, store.ErrSnapshotTarget
	}
	if release.Ref != "" && (snapshot.Release == nil || snapshot.Release.Identity != release) {
		return nil, store.ErrSnapshotTarget
	}
	if snapshot.Track != nil && snapshot.Track.Release != nil && snapshot.Release != nil && *snapshot.Track.Release != snapshot.Release.Identity {
		return nil, store.ErrSnapshotTarget
	}
	return s.storeEditPreview(*snapshot)
}

func (s *Service) PreviewCurrentEditUpstream(trackID int64) (*domain.EditPreview, error) {
	session, err := s.store.ReadEditorSession(trackID)
	if err != nil {
		return nil, err
	}
	var snapshot domain.Snapshot
	if err := json.Unmarshal([]byte(session.SnapshotJSON), &snapshot); err != nil {
		return nil, err
	}
	if snapshot.Track == nil {
		return nil, nil
	}
	if f := session.Upstream["track.lyrics"]; f.State == domain.Value {
		snapshot.Track.Lyrics.State = domain.Value
		if err := json.Unmarshal([]byte(f.ValueJSON), &snapshot.Track.Lyrics.Value); err != nil {
			return nil, err
		}
	}
	return s.storeEditPreview(snapshot)
}

func (s *Service) EditArtworkTarget(previewID string) (*domain.Identity, error) {
	snapshot, err := s.editPreview(previewID)
	if err != nil {
		return nil, err
	}
	if snapshot.Release == nil || snapshot.Release.Artwork.State != domain.Value || len(snapshot.Release.Artwork.Value) == 0 {
		return nil, nil
	}
	target := snapshot.Release.Identity
	return &target, nil
}
