package importer

import (
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestSummarySeparatesExistingSkipsFromNewTracksAndProblems(t *testing.T) {
	st := openStore(t)
	r := newRunner(st, &recordingSearcher{})
	batch := enqueueOne(t, st, "Song", "Artist", "Album")
	// This fixture isolates reporting; production skip transactions are tested
	// in automatic admission/Runner integration tests.
	if _, err := st.DB().Exec("UPDATE import_item SET status='skipped',resolution_reason=? WHERE batch_id=?", store.ResolutionExistingTrack, batch); err != nil {
		t.Fatal(err)
	}
	line := r.summaryFor(batch)
	for _, want := range []string{"新增收藏 0 首", "已存在跳过 1 行", "其他跳过 0 行", "待复核 0 行", "本批涉及 0 个问题 / 0 行来源"} {
		if !containsStr(line, want) {
			t.Fatalf("missing %q in %q", want, line)
		}
	}
}
