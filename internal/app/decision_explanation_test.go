package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestReviewDecisionReasonsAreReadableAndReadOnly(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	id := enqueueOneItem(t, a, "Song", "Artist", "Album")
	reasons := []matching.DecisionReason{{Code: "duration_conflict", Field: "duration"}, {Code: "position_ambiguous"}, {Code: "target_album_unresolved"}, {Code: "prefer_owned_compatible_track"}, {Code: "future_code", Detail: "not a user message"}}
	body, _ := json.Marshal(struct {
		Reasons []matching.DecisionReason `json:"reasons"`
	}{reasons})
	if err := a.store.MarkReview(id, store.ReviewAmbiguous, "[]", "[]", string(body)); err != nil {
		t.Fatal(err)
	}
	before, err := a.store.GetItem(id)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := a.GetReviewProblemQueue()
	if err != nil || len(queue.Items) != 1 || len(queue.Items[0].Sources) != 1 {
		t.Fatal(queue, err)
	}
	got := queue.Items[0].Sources[0].DecisionReasons
	if len(got) != len(reasons) {
		t.Fatal(got)
	}
	for i, reason := range got {
		if reason.Code != reasons[i].Code || reason.Message == "" || reason.Message == reason.Code || strings.Contains(reason.Message, "not a user message") {
			t.Fatal(reason)
		}
	}
	if got[3].Message != "未指定细发行，优先已收藏的兼容版本" {
		t.Fatal(got)
	}
	after, err := a.store.GetItem(id)
	if err != nil || *after.ReviewEvidence != string(body) || after.Revision != before.Revision {
		t.Fatal("read rewrote evidence", after, err)
	}
}
