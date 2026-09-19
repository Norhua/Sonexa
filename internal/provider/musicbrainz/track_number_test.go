package musicbrainz

import (
	"encoding/json"
	"testing"
)

func TestCandidateTrackNumberFallsBackToNumericNumber(t *testing.T) {
	for _, tc := range []struct {
		position int
		number   string
		want     int
	}{
		{0, "2", 2}, {3, "2", 3}, {0, "A1", 0}, {0, "", 0}, {0, "0", 0},
	} {
		// 回放搜索 JSON，不拿已填充的 Candidate 遮住解析缺口。
		body, err := json.Marshal(map[string]any{"recordings": []any{map[string]any{
			"id": "rec", "title": "song", "releases": []any{map[string]any{
				"id": "rel", "media": []any{map[string]any{
					"track-count": 12, "track": []any{map[string]any{
						"id": "trk", "position": tc.position, "number": tc.number,
					}},
				}},
			}},
		}}})
		if err != nil {
			t.Fatal(err)
		}
		var response searchResponse
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatal(err)
		}
		rows := expandToTrackCandidates(response.Recordings)
		if len(rows) != 1 {
			t.Fatal(rows)
		}
		got := rows[0]
		if tc.want == 0 {
			if got.TrackNumber != nil {
				t.Fatalf("must not guess track number for %+v", tc)
			}
		} else if got.TrackNumber == nil || *got.TrackNumber != tc.want {
			t.Fatalf("track number = %v for %+v", got.TrackNumber, tc)
		}
		if got.TrackTotal == nil || *got.TrackTotal != 12 {
			t.Fatalf("lost total: %+v", got)
		}
	}
}
