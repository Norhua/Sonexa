package matching

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func sptr(s string) *string { return &s }
func iptr(i int) *int       { return &i }

// TestCompareTextThreeLevels 覆盖 4.1：精确 / 模糊 / 不匹配三级判定。
func TestCompareTextThreeLevels(t *testing.T) {
	w := DefaultWeights()
	cases := []struct {
		name string
		a, b string
		want MatchDegree
	}{
		{"标准化后相等即精确", "That Girl", "ＴＨＡＴ　ＧＩＲＬ", MatchExact},
		{"假名精确", "ハルヒ", "はるひ", MatchExact},
		{"词元包含即模糊", "God knows", "God knows Live", MatchFuzzy},
		{"前缀截断即模糊", "Instrumental Version", "Instrument", MatchFuzzy},
		{"高相似度即模糊", "Natalia Lesz", "Natalia Leszh", MatchFuzzy},
		{"完全不同即不匹配", "That Girl", "冒険でしょでしょ", MatchNone},
		{"空串不匹配", "", "anything", MatchNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := w.CompareText(c.a, c.b); got != c.want {
				t.Errorf("CompareText(%q,%q) = %d, 期望 %d", c.a, c.b, got, c.want)
			}
		})
	}
}

// TestTitleMismatchDiscarded 覆盖 4.2：曲名不匹配即丢弃，不计分。
func TestTitleMismatchDiscarded(t *testing.T) {
	w := DefaultWeights()
	q := provider.Query{Title: "That Girl", Artist: "Liu", Album: "That Girl"}
	c := provider.Candidate{
		Title:      sptr("完全不同的曲名"),
		ArtistName: sptr("Liu"),
		AlbumTitle: sptr("That Girl"),
	}
	got := w.ScoreCandidate(q, c)
	if !got.Rejected {
		t.Error("曲名不匹配的候选应被丢弃")
	}
	if got.Total != 0 {
		t.Errorf("被丢弃的候选不应计分, 得到 %v", got.Total)
	}
}

// TestMusicBrainzScoringFullMarks 覆盖 4.3：MB 满分 100。
func TestMusicBrainzScoringFullMarks(t *testing.T) {
	w := DefaultWeights()
	q := provider.Query{Title: "That Girl", Artist: "Natalia Lesz", Album: "That Girl"}
	c := provider.Candidate{
		Provider:      "musicbrainz",
		Title:         sptr("That Girl"),
		ArtistName:    sptr("Natalia Lesz"),
		AlbumTitle:    sptr("That Girl"),
		UpstreamScore: iptr(100), // 100 × 0.05 = 5，恰好封顶
	}
	got := w.ScoreCandidate(q, c)
	if got.Total != 100 {
		t.Errorf("MB 满分候选得分 = %v, 期望 100", got.Total)
	}
	if !got.HighConf {
		t.Error("曲名+署名+歌集全精确应为高置信")
	}
}

// TestMusicBrainzUpstreamScoreCap 覆盖 4.9/6.9：上游 score × 0.05，上限 5。
func TestMusicBrainzUpstreamScoreCap(t *testing.T) {
	w := DefaultWeights()
	q := provider.Query{Title: "X", Artist: "Y", Album: "Z"}
	base := provider.Candidate{Title: sptr("X"), ArtistName: sptr("Y"), AlbumTitle: sptr("Z")}

	base.UpstreamScore = iptr(60) // 60 × 0.05 = 3
	if got := w.ScoreCandidate(q, base); got.Total != 95+3 {
		t.Errorf("score=60 时得分 = %v, 期望 98", got.Total)
	}
	base.UpstreamScore = iptr(200) // 200 × 0.05 = 10，封顶为 5
	if got := w.ScoreCandidate(q, base); got.Total != 100 {
		t.Errorf("score=200 时得分 = %v, 期望 100（封顶 5）", got.Total)
	}
}

// TestITunesScoringWithDuration 覆盖 4.4：iTunes 用时长补足那 5 分，满分同为 100。
func TestITunesScoringWithDuration(t *testing.T) {
	w := DefaultWeights()
	q := provider.Query{
		Title: "That Girl", Artist: "Natalia Lesz", Album: "That Girl",
		DurationMS: iptr(221000),
	}
	c := provider.Candidate{
		Provider:   "itunes",
		Title:      sptr("That Girl"),
		ArtistName: sptr("Natalia Lesz"),
		AlbumTitle: sptr("That Girl"),
		DurationMS: iptr(222000), // Δ = 1s ≤ 3s，得 5
	}
	got := w.ScoreCandidate(q, c)
	if got.Total != 100 {
		t.Errorf("iTunes 满分候选得分 = %v, 期望 100", got.Total)
	}

	// Δ 在 3~10s 得 2
	c.DurationMS = iptr(228000) // Δ = 7s
	if got := w.ScoreCandidate(q, c); got.Total != 97 {
		t.Errorf("Δ=7s 得分 = %v, 期望 97", got.Total)
	}
	// Δ > 10s 不得分
	c.DurationMS = iptr(240000) // Δ = 19s
	if got := w.ScoreCandidate(q, c); got.Total != 95 {
		t.Errorf("Δ=19s 得分 = %v, 期望 95", got.Total)
	}
}

// TestITunesNoDurationNoBonus 覆盖 4.4：输入无时长时时长项不计分，也不扣分。
func TestITunesNoDurationNoBonus(t *testing.T) {
	w := DefaultWeights()
	q := provider.Query{Title: "X", Artist: "Y", Album: "Z"} // 无时长
	c := provider.Candidate{
		Provider: "itunes", Title: sptr("X"), ArtistName: sptr("Y"),
		AlbumTitle: sptr("Z"), DurationMS: iptr(200000),
	}
	if got := w.ScoreCandidate(q, c); got.Total != 95 {
		t.Errorf("输入无时长时得分 = %v, 期望 95（不计时长分也不扣分）", got.Total)
	}
}

// TestHighConfidenceIsStructural 覆盖 4.5：
// 79 分双模糊判为非高置信，75 分署名精确判为高置信。
func TestHighConfidenceIsStructural(t *testing.T) {
	w := DefaultWeights()

	// 79 分：曲名匹配 + 署名模糊(12) + 歌集模糊(12)，两处都只是模糊
	q1 := provider.Query{Title: "God knows", Artist: "Suzumiya Haruhi", Album: "TV animation"}
	c1 := provider.Candidate{
		Title:      sptr("God knows"),
		ArtistName: sptr("Suzumiya Haruhi feat Aya"), // 词元包含 → 模糊
		AlbumTitle: sptr("TV animation soundtrack"),  // 词元包含 → 模糊
	}
	got1 := w.ScoreCandidate(q1, c1)
	if got1.CreditMatch != MatchFuzzy || got1.AlbumMatch != MatchFuzzy {
		t.Fatalf("前置条件不成立: credit=%d album=%d", got1.CreditMatch, got1.AlbumMatch)
	}
	if got1.Total != 79 {
		t.Fatalf("双模糊得分 = %v, 期望 79", got1.Total)
	}
	if got1.HighConf {
		t.Error("79 分双模糊不应判为高置信——这是结构性条件而非分数线")
	}

	// 75 分：曲名匹配 + 署名精确(20)，无歌集
	q2 := provider.Query{Title: "That Girl", Artist: "Natalia Lesz", Album: ""}
	c2 := provider.Candidate{
		Title:      sptr("That Girl"),
		ArtistName: sptr("Natalia Lesz"), // 精确
	}
	got2 := w.ScoreCandidate(q2, c2)
	if got2.Total != 75 {
		t.Fatalf("署名精确得分 = %v, 期望 75", got2.Total)
	}
	if !got2.HighConf {
		t.Error("75 分署名精确应判为高置信")
	}
}

// TestJaroWinkler 校准相似度实现，锚定金样例期望。
func TestJaroWinkler(t *testing.T) {
	cases := []struct {
		a, b string
		min  float64
	}{
		{"martha", "marhta", 0.96},
		{"dixon", "dicksonx", 0.81},
		{"natalialesz", "natalialeszh", 0.95},
	}
	for _, c := range cases {
		got := jaroWinkler(c.a, c.b)
		if got < c.min {
			t.Errorf("jaroWinkler(%q,%q) = %.4f, 期望 ≥ %.2f", c.a, c.b, got, c.min)
		}
	}
	if jaroWinkler("abc", "xyz") > 0.5 {
		t.Error("完全不同的串相似度应很低")
	}
}
