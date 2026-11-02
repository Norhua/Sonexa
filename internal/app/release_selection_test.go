package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/library"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

const (
	ameRecording       = "fea2d6f4-0957-4090-baba-b013ae3dfbcd"
	ameGroup           = "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d"
	ameSelectedRelease = "07088ba6-2c40-356a-8d0c-ef72d8a21d93"
	ameSelectedTrack   = "5e5f141f-cdaa-3346-9f9a-43b572006e9b"
	ameLastRelease     = "e8ab0e85-213e-3a70-81b5-d6244a852945"
	ameLastTrack       = "7b6ec98d-1ebf-377e-ab95-1a2a7e0cf9b1"
)

// Real captured HTTP data → actual MB parser → Engine → library selection →
// SubmitImport/Runner/transaction. Not the legacy Engine.Decision classification.
// Default execution is entirely offline and never opens the user's database.
func TestSekaijuuNoAmeReleaseSelection(t *testing.T) {
	for _, in := range []matching.Input{
		{Title: "世界中の雨", Artist: "チームドラゴン from AKB48", Album: "心の羽根"},
		{Title: "世界中の雨(世间之雨)", Artist: "チームドラゴン from AKB48", Album: "心の羽根(ドラゴンボール改ver.)(心的翅膀)"},
		{Title: "世界中の雨（世间之雨）", Artist: "チームドラゴン from AKB48", Album: "心の羽根（ドラゴンボール改ver.）（心..."},
	} {
		t.Run("空库_"+in.Album, func(t *testing.T) {
			a, scored, requests := ameReplay(t, in)
			high := matching.AutomaticCandidates(scored)
			assertAmeCandidates(t, scored, high)
			// Log every actual candidate in comparison-key order, including the
			// track UUID. Sorting this diagnostic never chooses the result.
			ordered := slices.Clone(high)
			slices.SortFunc(ordered, compareAmeRelease)
			t.Logf("输入=%q / %q / %q", in.Title, in.Artist, in.Album)
			t.Log("阶段1：22 个搜索 track → 11 个原版自动候选；11 个 off vocal 仅供人工复核")
			for _, sc := range ordered {
				c := sc.Candidate
				t.Logf("release=%s track=%s 总曲数=%d 载体=%s 国家=%s 曲序=%d 分数=%.0f", *c.ReleaseRef, c.MatchRef, *c.ReleaseTrackTotal, *c.MediumFormat, *c.Country, *c.TrackNumber, sc.Score.Total)
			}
			for _, sc := range high {
				r, err := a.store.GetReleaseByRef("musicbrainz", *sc.Candidate.ReleaseRef)
				if err != nil || r != nil {
					t.Fatalf("空库预置不成立: %+v %v", r, err)
				}
			}
			t.Log("阶段2：同组、同录音且各发行只有一个目标位置，身份检查通过；库中已有发行=0")
			t.Log("阶段3：11 个发行的整发行总曲数均为 4，全部保留；国家均为 JP，仍全部保留；CD 不参与自动排序")
			trackIDs := make([]string, 0, len(high))
			for _, sc := range high {
				trackIDs = append(trackIDs, sc.Candidate.MatchRef)
			}
			slices.Sort(trackIDs)
			if trackIDs[0] == ameSelectedTrack {
				t.Fatal("fixture no longer distinguishes release-first from track-first order")
			}
			t.Logf("排序键核验：最小 track MBID=%s，但它不会胜出；release_ref 在 match_ref 之前比较", trackIDs[0])
			selection := ameSelect(t, a, scored)
			assertAmeSelected(t, selection, ameSelectedRelease, ameSelectedTrack)
			if selection.Plan.Baseline.ExistingReleaseID != 0 || selection.Plan.Baseline.RequiresConfirmation {
				t.Fatal(selection.Plan.Baseline)
			}
			t.Logf("阶段4：按 provider + release_ref + match_ref 升序兜底，选中 release=%s track=%s；首次建立组，无新增成员确认", *selection.Candidate.ReleaseRef, selection.Candidate.MatchRef)
			// Permuting every input position proves it is not “first API result”.
			for shift := range len(scored) {
				permuted := append(slices.Clone(scored[shift:]), scored[:shift]...)
				if shift%2 == 0 {
					slices.Reverse(permuted)
				}
				assertAmeSelected(t, ameSelect(t, a, permuted), ameSelectedRelease, ameSelectedTrack)
			}
			t.Logf("顺序对照：%d 组旋转/反转输入均选中同一发行", len(scored))
			ameSubmitAndAssert(t, a, in, ameSelectedRelease, ameSelectedTrack, 1)
			if requests.Load() != 1 {
				t.Fatalf("应只重放一次已含总曲数的搜索响应，不请求摘要/详情: %d", requests.Load())
			}
			t.Log("实际提交：imported=1、review=0、skipped=0；仅重放一次搜索，无摘要/详情请求；数据库绑定与选择一致")
		})
	}
}

// Counterfactuals deliberately modify the captured candidate facts or local
// state, rather than claiming these conditions occurred in the user's library.
func TestSekaijuuNoAmeSelectionCounterfactuals(t *testing.T) {
	in := matching.Input{Title: "世界中の雨", Artist: "チームドラゴン from AKB48", Album: "心の羽根"}
	for _, scenario := range []string{"已有非最小发行", "末个发行改为5首", "仅末个发行为非世界国家", "载体偏好不改变结果", "同发行多个位置必须复核", "不同录音必须复核"} {
		t.Run(scenario, func(t *testing.T) {
			a, scored, _ := ameReplay(t, in)
			high := matching.AutomaticCandidates(scored)
			assertAmeCandidates(t, scored, high)
			last := -1
			for i, sc := range high {
				if *sc.Candidate.ReleaseRef == ameLastRelease {
					last = i
				}
			}
			if last < 0 {
				t.Fatal("missing last release")
			}
			release, track := ameLastRelease, ameLastTrack
			switch scenario {
			case "已有非最小发行":
				// Another song owns that release, not the song being imported.
				// Synthetic sibling identity avoids duplicate-track short-circuit.
				sibling := high[last].Candidate
				sibling.MatchRef = "00000000-0000-4000-8000-000000000001"
				sibling.Title = strPtr("测试用既有发行内另一首歌曲")
				sibling.TrackNumber = intPtr(1)
				sibling.RecordingRef = strPtr("00000000-0000-4000-8000-000000000002")
				plan, err := a.library.PrepareCandidate(context.Background(), sibling, 0)
				if err != nil {
					t.Fatal(err)
				}
				owned, err := a.library.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{})
				if err != nil {
					t.Fatal(err)
				}
				selected := ameSelect(t, a, scored)
				assertAmeSelected(t, selected, release, track)
				if selected.Plan.Baseline.ExistingReleaseID != owned.ReleaseID || selected.Plan.Baseline.RequiresConfirmation {
					t.Fatal(selected.Plan.Baseline)
				}
				ameSubmitAndAssert(t, a, in, release, track, 2)
				t.Logf("已有发行优先：预置 %s 的另一首歌后，实际导入选择 %s，而不是 %s", release, release, ameSelectedRelease)
				return
			case "末个发行改为5首":
				high[last].Candidate.ReleaseTrackTotal = intPtr(5)
			case "仅末个发行为非世界国家":
				for i := range high {
					high[i].Candidate.Country = strPtr("XW")
				}
				high[last].Candidate.Country = strPtr("JP")
			case "载体偏好不改变结果":
				cfg := matching.DefaultLadderConfig()
				cfg.MediumPriority = []string{"Digital Media", "CD"}
				a.store.SetLadderConfig(cfg)
				high[last].Candidate.MediumFormat = strPtr("Digital Media")
				release, track = ameSelectedRelease, ameSelectedTrack
			case "同发行多个位置必须复核":
				extra := high[last]
				extra.Candidate.MatchRef = "00000000-0000-4000-8000-000000000003"
				extra.Candidate.TrackNumber = intPtr(3)
				high = append(high, extra)
			case "不同录音必须复核":
				high[last].Candidate.RecordingRef = strPtr("00000000-0000-4000-8000-000000000004")
			}
			selected := ameSelect(t, a, high)
			if scenario == "同发行多个位置必须复核" || scenario == "不同录音必须复核" {
				if selected.Candidate != nil || selected.Reason != library.ReasonAmbiguous {
					t.Fatalf("%+v", selected)
				}
				t.Logf("反事实 %s → review: %s", scenario, selected.Reason)
				return
			}
			assertAmeSelected(t, selected, release, track)
			t.Logf("反事实 %s → release=%s track=%s", scenario, release, track)
		})
	}
}

func TestSekaijuuNoAmeSelectedReleaseDetail(t *testing.T) {
	body, err := os.ReadFile("../../testdata/golden/responses/musicbrainz/release_kokoro_no_hane_07088ba6.json")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := musicbrainz.ParseDetailJSON(body, ameSelectedTrack)
	if err != nil {
		t.Fatal(err)
	}
	if detail.ReleaseRef == nil || *detail.ReleaseRef != ameSelectedRelease || detail.MatchRef != ameSelectedTrack || detail.RecordingRef == nil || *detail.RecordingRef != ameRecording || detail.ReleaseTrackTotal == nil || *detail.ReleaseTrackTotal != 4 || len(detail.Tracklist) != 4 || detail.TrackNumber == nil || *detail.TrackNumber != 2 {
		t.Fatalf("wrong detail: %+v", detail)
	}
	if detail.CatalogNumber == nil || *detail.CatalogNumber != "COZA-457" {
		t.Fatal(detail.CatalogNumber)
	}
	if detail.DiscNumber == nil || *detail.DiscNumber != 1 || detail.ReleaseDate == nil || *detail.ReleaseDate != "2010-07-21" {
		t.Fatalf("missing release context: %+v", detail)
	}
	t.Logf("真实详情交叉核验：release=%s track=%s disc=%d position=%d/4 厂牌目录号=%s 日期=%s", *detail.ReleaseRef, detail.MatchRef, *detail.DiscNumber, *detail.TrackNumber, *detail.CatalogNumber, *detail.ReleaseDate)
}

func intPtr(v int) *int { return &v }

func ameReplay(t *testing.T, in matching.Input) (*App, []matching.ScoredCandidate, *atomic.Int32) {
	t.Helper()
	body, err := os.ReadFile("../../testdata/golden/responses/musicbrainz/search_sekaijuu_no_ame.json")
	if err != nil {
		t.Fatal(err)
	}
	requests := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/ws/2/recording" || r.URL.Query().Get("query") == "" {
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	a, cleanup := newTestApp(t)
	t.Cleanup(cleanup)
	a.enricher.Pause()
	// Accelerate only the local replay server, never the public MB endpoint.
	a.limiters.Configure(provider.MusicBrainz, 1000, time.Second)
	client := provider.NewClient(a.limiters, a.store.AsProviderCache(), "Sonexa/test ( test@example.com )")
	p := musicbrainz.New(client, musicbrainz.Config{Contact: "test@example.com", ServerURL: srv.URL}, store.ParamsHash)
	a.registry.Register(p)
	a.rebuildRunner(a.settings.Get())
	res, err := matching.NewEngine(a.settings.Get().Matching).Run(context.Background(), p, a.store, in)
	if err != nil {
		t.Fatal(err)
	}
	return a, res.Scored, requests
}

func assertAmeCandidates(t *testing.T, all, high []matching.ScoredCandidate) {
	t.Helper()
	if len(all) != 22 || len(high) != 11 {
		t.Fatalf("all=%d automatic=%d", len(all), len(high))
	}
	releases := map[string]bool{}
	tracks := map[string]bool{}
	for _, sc := range high {
		c := sc.Candidate
		if c.ReleaseRef == nil || c.RecordingRef == nil || *c.RecordingRef != ameRecording || c.ReleaseGroupRef == nil || *c.ReleaseGroupRef != ameGroup || c.ReleaseTrackTotal == nil || *c.ReleaseTrackTotal != 4 || c.Country == nil || *c.Country != "JP" || c.MediumFormat == nil || *c.MediumFormat != "CD" || c.TrackNumber == nil || *c.TrackNumber != 2 || sc.Score.ReviewOnly {
			t.Fatalf("candidate facts: %+v score=%+v", c, sc.Score)
		}
		if sc.Score.Total != high[0].Score.Total {
			t.Fatal("fixture scores are no longer tied")
		}
		if releases[*c.ReleaseRef] || tracks[c.MatchRef] {
			t.Fatal("not eleven distinct release positions")
		}
		releases[*c.ReleaseRef] = true
		tracks[c.MatchRef] = true
	}
	for _, sc := range all {
		if sc.Candidate.RecordingRef != nil && *sc.Candidate.RecordingRef != ameRecording && !sc.Score.ReviewOnly {
			t.Fatal("other recording became automatic")
		}
	}
}
func ameSelect(t *testing.T, a *App, scored []matching.ScoredCandidate) library.Selection {
	t.Helper()
	selection, err := a.library.SelectCandidate(context.Background(), scored, 0)
	if err != nil {
		t.Fatal(err)
	}
	return selection
}
func assertAmeSelected(t *testing.T, selection library.Selection, release, track string) {
	t.Helper()
	if selection.Reason != "" || selection.Candidate == nil || selection.Candidate.ReleaseRef == nil || *selection.Candidate.ReleaseRef != release || selection.Candidate.MatchRef != track || selection.Plan == nil {
		t.Fatalf("wanted %s / %s; selection=%+v", release, track, selection)
	}
}
func compareAmeRelease(a, b matching.ScoredCandidate) int {
	// Only orders the printed evidence, not the selection under test.
	return strings.Compare(*a.Candidate.ReleaseRef, *b.Candidate.ReleaseRef)
}
func ameSubmitAndAssert(t *testing.T, a *App, in matching.Input, release, track string, total int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.csv")
	if err := os.WriteFile(path, []byte(fmt.Sprintf("title,artist,album\n%s,%s,%s\n", in.Title, in.Artist, in.Album)), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := a.SubmitImport(path, "", []string{"title", "artist", "album"}, "UTF-8", ",", true)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "真实导入提交", func() bool { return !a.runner.IsRunning(batch) })
	stats, err := a.store.Stats(batch)
	if err != nil || stats.Imported != 1 || stats.Review != 0 || stats.Skipped != 0 {
		t.Fatal(stats, err)
	}
	id, err := a.store.FindByMatchRef("musicbrainz", track)
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	meta, err := a.store.GetMetadata(id)
	if err != nil || meta == nil || meta.ReleaseRef == nil || *meta.ReleaseRef != release {
		t.Fatal(meta, err)
	}
	n, err := a.store.CountTracks(store.TrackFilter{})
	if err != nil || n != total {
		t.Fatal(n, err)
	}
}
