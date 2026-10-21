package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/provider"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
)

const EventLyricsProgress = "lyrics:progress"

// Shared across App/service snapshots, including settings rebuilds.
var processLyricsFetchPool = lyrics.NewFetchPool(4, 1)

type lyricFlight struct {
	done     chan struct{}
	started  bool
	document *lyricdoc.Document
	err      error
}

type LyricsProgress struct {
	SessionID  string                    `json:"sessionId"`
	Source     string                    `json:"source"`
	Sequence   uint64                    `json:"sequence"`
	Phase      string                    `json:"phase"`
	Searched   int                       `json:"searched"`
	Selected   int                       `json:"selected"`
	Success    int                       `json:"success"`
	NoLyrics   int                       `json:"noLyrics"`
	Failed     int                       `json:"failed"`
	Incomplete int                       `json:"incomplete"`
	Candidates []lyrics.CandidateSummary `json:"candidates"`
}

func pendingLyricChannel(state string) lyrics.ChannelSummary {
	return lyrics.ChannelSummary{Status: state, Origin: "unknown", SyncStatus: "unknown"}
}

func (s *lyricSession) snapshotLocked(id string) LyricsProgress {
	out := LyricsProgress{SessionID: id, Source: s.source, Sequence: s.sequence, Phase: s.phase, Searched: len(s.order), Selected: len(s.top), Candidates: []lyrics.CandidateSummary{}}
	for _, key := range s.order {
		item := s.summaries[key]
		out.Candidates = append(out.Candidates, lyrics.CopySummary(item))
		if !item.Prefetch {
			continue
		}
		switch item.FetchState {
		case "ready":
			out.Success++
		case "no_lyrics":
			out.NoLyrics++
		case "error":
			out.Failed++
		default:
			out.Incomplete++
		}
	}
	return out
}

func (a *App) LyricsSearchSnapshot(id string) (*LyricsProgress, error) {
	s, _, err := a.lyricWork(id)
	if err != nil {
		return nil, err
	}
	defer a.lyricWorkers.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.snapshotLocked(id)
	return &out, nil
}

// StartLyricsPrefetch accepts no candidate IDs or budgets. The search froze
// the top three before any content score was available; retries do not expand it.
func (a *App) StartLyricsPrefetch(id string) (*LyricsProgress, error) {
	s, _, err := a.lyricWork(id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		a.lyricWorkers.Done()
		return nil, err
	}
	if s.prefetchStarted || s.phase == "done" {
		out := s.snapshotLocked(id)
		s.mu.Unlock()
		a.lyricWorkers.Done()
		return &out, nil
	}
	if s.phase != "ready" {
		s.mu.Unlock()
		a.lyricWorkers.Done()
		return nil, fmt.Errorf("请等待本来源搜索完成")
	}
	s.prefetchStarted = true
	s.phase = "prefetching"
	for _, key := range s.top {
		item := s.summaries[key]
		if item.FetchState != "pending" {
			continue
		}
		s.inflight[key] = &lyricFlight{done: make(chan struct{})}
		item.FetchState = "queued"
		s.summaries[key] = item
	}
	s.sequence++
	out := s.snapshotLocked(id)
	s.mu.Unlock()
	a.emitEvent(EventLyricsProgress, out)
	// Transfer the already registered work to the worker; shutdown cannot
	// pass its Wait between accepting a start and registering its goroutine.
	go func() { defer a.lyricWorkers.Done(); a.runLyricsPrefetch(id, s) }()
	return &out, nil
}

func (a *App) runLyricsPrefetch(id string, s *lyricSession) {
	var stopped error
	for _, key := range s.top {
		s.mu.Lock()
		state := s.summaries[key].FetchState
		s.mu.Unlock()
		if state != "queued" && state != "loading" {
			continue
		}
		_, err := a.fetchLyricDocument(id, s, key, true, false)
		if stop := s.roundBudget.Err(); stop != nil {
			stopped = stop
			break
		}
		var failure *lyrics.FetchError
		if err != nil && (s.ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, provider.ErrSendBudget) || errors.As(err, &failure) && failure.StopSource) {
			stopped = err
			break
		}
	}
	s.mu.Lock()
	if stopped != nil {
		for _, key := range s.top {
			flight := s.inflight[key]
			if flight == nil || flight.started {
				continue
			}
			item := s.summaries[key]
			item.FetchState = "skipped"
			item.ErrorKind = "source_stopped"
			item.StopReason = lyricFailureKind(stopped)
			s.summaries[key] = item
			flight.err = stopped
			close(flight.done)
			delete(s.inflight, key)
		}
		s.phase = "stopped"
		if lyricRestriction(stopped) {
			s.restricted = stopped
		}
	} else {
		s.phase = "done"
		for _, key := range s.top {
			if s.summaries[key].FetchState != "ready" {
				s.phase = "partial"
			}
		}
	}
	s.roundCancel()
	s.sequence++
	out := s.snapshotLocked(id)
	s.mu.Unlock()
	a.emitEvent(EventLyricsProgress, out)
}

func lyricRoundFinished(phase string) bool {
	return phase == "done" || phase == "partial" || phase == "stopped"
}

func lyricRestriction(err error) bool {
	var failure *lyrics.FetchError
	return errors.As(err, &failure) && failure.StopSource && failure.Kind != "timeout" && failure.Kind != "cancelled" && failure.Kind != "budget"
}

func lyricFailureKind(err error) string {
	var failure *lyrics.FetchError
	if errors.As(err, &failure) {
		return failure.Kind
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, provider.ErrSendBudget):
		return "budget"
	case errors.Is(err, lyrics.ErrRetentionLimit):
		return "memory_limit"
	case errors.Is(err, lyricdoc.ErrTooLarge):
		return "too_large"
	case errors.Is(err, lyricdoc.ErrNoLyrics):
		return "no_lyrics"
	default:
		return "fetch"
	}
}

func lyricCandidateContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 40*time.Second)
}

func waitLyricFlight(ctx context.Context, flight *lyricFlight) (*lyricdoc.Document, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.done:
		return flight.document, flight.err
	}
}

func (a *App) fetchLyricDocument(id string, s *lyricSession, key string, automatic, refresh bool) (*lyricdoc.Document, error) {
	s.mu.Lock()
	// A close can happen after work registration but before this worker starts.
	// The prefetch runner completes unstarted reservations; never recreate
	// approval state or enter loading after the session has been released.
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if flight := s.inflight[key]; flight != nil && (!automatic || flight.started) {
		s.mu.Unlock()
		return waitLyricFlight(s.ctx, flight)
	}
	if d := s.documents[key]; d != nil && !refresh {
		s.mu.Unlock()
		return d, nil
	}
	candidate, ok := s.candidates[key]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("所选歌词不属于当前搜索")
	}
	flight := s.inflight[key]
	if !automatic && !lyricRoundFinished(s.phase) {
		s.mu.Unlock()
		return nil, fmt.Errorf("请等待本来源预取结束后手动获取")
	}
	if !automatic && s.restricted != nil {
		err := s.restricted
		s.mu.Unlock()
		return nil, err
	}
	if flight == nil {
		flight = &lyricFlight{done: make(chan struct{})}
		s.inflight[key] = flight
	}
	flight.started = true
	s.previewed[key] = 0
	workCtx := s.ctx
	budget := s.roundBudget
	if automatic {
		workCtx = s.roundCtx
	} else {
		budget = provider.NewSendBudget(8)
		workCtx = provider.WithSendBudget(workCtx, budget)
	}
	workCtx, cancel := lyricCandidateContext(workCtx)
	defer cancel()
	item := s.summaries[key]
	previous := item
	item.Candidate = candidate
	item.DocumentVersion = 0
	item.Format = ""
	item.FetchState = "loading"
	item.ErrorKind = ""
	item.StopReason = ""
	item.Translation = pendingLyricChannel("loading")
	item.Romanization = pendingLyricChannel("loading")
	item.WordTiming = "loading"
	s.summaries[key] = item
	s.sequence++
	progress := s.snapshotLocked(id)
	s.mu.Unlock()
	a.emitEvent(EventLyricsProgress, progress)
	finish := a.beginInteractive()
	var doc *lyricdoc.Document
	release, err := processLyricsFetchPool.Acquire(workCtx, s.source)
	checked := false
	if err == nil {
		defer release()
		// Another explicit operation may have restricted this source while
		// this candidate was queued in the process pool.
		s.mu.Lock()
		err = s.restricted
		s.mu.Unlock()
		if err == nil {
			checked = true
			doc, err = s.service.Fetch(workCtx, candidate)
		}
	}
	finish()
	s.mu.Lock()
	if stop := budget.Err(); lyricRestriction(stop) {
		s.restricted = stop
	}
	if s.ctx.Err() != nil {
		doc, err = nil, s.ctx.Err()
	}
	if err == nil {
		oldBytes, newBytes := lyricdoc.RetainedBytes(s.documents[key]), lyricdoc.RetainedBytes(doc)
		err = s.retention.Replace(s.retainedBytes, oldBytes, newBytes)
		if err == nil {
			s.retainedBytes += newBytes - oldBytes
		}
	}
	item = s.summaries[key]
	if err == nil {
		s.documents[key] = doc
		s.versions[key]++
		s.previewed[key] = 0
		item.Candidate = lyrics.WithContentScore(candidate, doc, s.query.DurationMS)
		item.FetchState = "ready"
		item.Format = doc.Format
		item.WordTiming = doc.WordTiming
		item.DocumentVersion = s.versions[key]
		item.Translation = lyrics.SummarizeChannel(doc.Translation)
		item.Romanization = lyrics.SummarizeChannel(doc.Romanization)
	} else {
		doc = nil // rejected documents must not survive in completed flights
		item.FetchState = "error"
		item.ErrorKind = lyricFailureKind(err)
		if lyricRestriction(err) {
			s.restricted = err
		}
		status := "error"
		if !checked {
			item.FetchState = "skipped"
			item.StopReason = item.ErrorKind
			status = "pending"
		}
		if errors.Is(err, lyricdoc.ErrNoLyrics) {
			item.FetchState = "no_lyrics"
			item.ErrorKind = "no_lyrics"
			status = "unknown"
		}
		item.Translation = pendingLyricChannel(status)
		item.Romanization = pendingLyricChannel(status)
		item.WordTiming = status
		// A failed refresh never evicts a retained version. Its old approval
		// was cleared above and must be explicitly acknowledged again.
		if s.documents[key] != nil {
			previous.ErrorKind, previous.StopReason = item.ErrorKind, item.StopReason
			item = previous
		}
	}
	s.summaries[key] = item
	flight.document, flight.err = doc, err
	delete(s.inflight, key)
	close(flight.done)
	s.sequence++
	progress = s.snapshotLocked(id)
	s.mu.Unlock()
	a.emitEvent(EventLyricsProgress, progress)
	return doc, err
}
