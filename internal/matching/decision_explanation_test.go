package matching

import (
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
)

func TestDecisionExplanationsKeepCodesAndDoNotExposeDiagnosticsAsMessages(t *testing.T) {
	input := []DecisionReason{{Code: "duration_conflict", Field: "duration", Detail: "remote:source:track"}, {Code: "position_ambiguous"}, {Code: "target_album_unresolved"}, {Code: "prefer_owned_compatible_track"}, {Code: "future_code", Detail: "internal_failure"}}
	before := append([]DecisionReason(nil), input...)
	got := ExplainReasons(append(input[:len(input):len(input)], input[0]))
	if !reflect.DeepEqual(input, before) || len(got) != len(input) {
		t.Fatal(input, got)
	}
	for i, r := range got {
		if r.Code != input[i].Code || r.Detail != input[i].Detail || r.Message == "" || r.Message == r.Code || strings.Contains(r.Message, "remote:") {
			t.Fatal(r)
		}
	}
	summary := DecisionSummary(&domain.DecisionEvidence{Reasons: got})
	if !strings.Contains(summary, "未指定细发行，优先已收藏的兼容版本") || strings.Contains(summary, "internal_failure") || strings.Contains(summary, "duration_conflict") {
		t.Fatal(summary)
	}
}
