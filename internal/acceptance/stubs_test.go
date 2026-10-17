package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// stubProvider 是一个最小的上游实现，用于验证「新增上游零页面代码改动」。
type stubProvider struct {
	id   provider.ProviderID
	name string
	caps provider.Capabilities
}

func (s *stubProvider) ID() provider.ProviderID             { return s.id }
func (s *stubProvider) DisplayName() string                 { return s.name }
func (s *stubProvider) Capabilities() provider.Capabilities { return s.caps }
func (s *stubProvider) RateLimit() (int, time.Duration)     { return 1, time.Second }
func (s *stubProvider) IsConfigured() error                 { return nil }
func (s *stubProvider) SearchTracks(context.Context, provider.Query) ([]provider.Candidate, error) {
	return nil, nil
}
func (s *stubProvider) FetchDetail(context.Context, provider.Ref) (*provider.Detail, error) {
	return nil, nil
}

var _ provider.MetadataProvider = (*stubProvider)(nil)

// httpSearcher 把查询直接发往 stub server，用于端到端验证出错退避与统计。
type httpSearcher struct {
	provider.MetadataProvider
	baseURL string
}

func (h *httpSearcher) SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
	u := fmt.Sprintf("%s/ws/2/recording?query=%s", h.baseURL, url.QueryEscape(q.Title))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &provider.HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Recordings []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"recordings"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	out := make([]provider.Candidate, 0, len(parsed.Recordings))
	for _, r := range parsed.Recordings {
		title := r.Title
		out = append(out, provider.Candidate{
			Provider: provider.MusicBrainz, MatchRef: r.ID, Title: &title,
		})
	}
	return out, nil
}

func (s *stubProvider) Refs() provider.RefCodec { return nil }
func (s *stubProvider) SnapshotFromCandidate(c provider.Candidate) (*domain.Snapshot, error) {
	return provider.CandidateSnapshot(s.ID(), c)
}
func (s *stubProvider) Complete(context.Context, domain.DetailTarget) (*domain.Snapshot, error) {
	return nil, provider.ErrUnsupported
}
func (s *stubProvider) LoadReleaseSummary(context.Context, domain.ReleaseIdentity) (*domain.ReleaseSummary, error) {
	return nil, provider.ErrUnsupported
}

func (s *httpSearcher) ID() provider.ProviderID { return provider.MusicBrainz }
func (s *httpSearcher) SnapshotFromCandidate(c provider.Candidate) (*domain.Snapshot, error) {
	return provider.CandidateSnapshot(s.ID(), c)
}
func (s *httpSearcher) LoadReleaseSummary(context.Context, domain.ReleaseIdentity) (*domain.ReleaseSummary, error) {
	return nil, provider.ErrUnsupported
}
