package lyrics

import (
	"encoding/json"
	"math/rand"
	"os"
	"reflect"
	"testing"
)

func TestSharedRankingFixture(t *testing.T) {
	body, err := os.ReadFile("../../testdata/lyrics/ranking.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Rows     []Candidate
		Expected []string
	}
	if err := json.Unmarshal(body, &f); err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 30; i++ {
		r.Shuffle(len(f.Rows), func(i, j int) { f.Rows[i], f.Rows[j] = f.Rows[j], f.Rows[i] })
		got := []string{}
		for _, c := range RankFinal(f.Rows) {
			got = append(got, c.ID)
		}
		if !reflect.DeepEqual(got, f.Expected) {
			t.Fatal(got)
		}
	}
	q := Query{Title: "Song", Artist: "Artist"}
	rows := rankCandidates(q, []Candidate{{Ref: "bad", Title: "Song (off vocal)", Artist: q.Artist}})
	if len(rows) != 0 {
		t.Fatal("version rejection bypassed")
	}
}
