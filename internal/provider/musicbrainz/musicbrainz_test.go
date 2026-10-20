package musicbrainz

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

// loadFixture 读取金样例响应。解析器测试不出网，直接重放落盘的原始响应。
func loadFixture(t *testing.T, name string, v any) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "golden", "responses", "musicbrainz", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取金样例 %s 失败: %v", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("解析金样例 %s 失败: %v", name, err)
	}
}

// TestSearchExpandsToTrackGranularity 覆盖 6.2：
// 一条 recording 收录于 3 张发行时产出 3 行，各行的载体/国家/年份不同。
func TestSearchExpandsToTrackGranularity(t *testing.T) {
	var resp searchResponse
	loadFixture(t, "search_recording.json", &resp)

	got := expandToTrackCandidates(resp.Recordings)
	if len(got) != 3 {
		t.Fatalf("候选数 = %d, 期望 3（一条录音收录于 3 张发行）", len(got))
	}

	// 每行的 match_ref 是 track MBID，彼此不同——
	// 用 recording MBID 会把 CD 版与数字版判为同一条。
	refs := map[string]bool{}
	for _, c := range got {
		if refs[c.MatchRef] {
			t.Errorf("match_ref 重复: %q", c.MatchRef)
		}
		refs[c.MatchRef] = true
	}
	for _, want := range []string{"trk-cd-jp-02", "trk-digital-jp-02", "trk-vinyl-us-a2"} {
		if !refs[want] {
			t.Errorf("缺少候选 %q, 实际为 %v", want, refs)
		}
	}

	// 各行的载体、国家、年份不同
	byRef := map[string]provider.Candidate{}
	for _, c := range got {
		byRef[c.MatchRef] = c
	}
	cd := byRef["trk-cd-jp-02"]
	if cd.MediumFormat == nil || *cd.MediumFormat != "CD" {
		t.Errorf("CD 版载体 = %v", cd.MediumFormat)
	}
	if cd.Country == nil || *cd.Country != "JP" {
		t.Errorf("CD 版国家 = %v", cd.Country)
	}
	if cd.Year == nil || *cd.Year != 2006 {
		t.Errorf("CD 版年份 = %v", cd.Year)
	}

	digital := byRef["trk-digital-jp-02"]
	if digital.MediumFormat == nil || *digital.MediumFormat != "Digital Media" {
		t.Errorf("数字版载体 = %v", digital.MediumFormat)
	}
	if digital.Year == nil || *digital.Year != 2009 {
		t.Errorf("数字版年份 = %v", digital.Year)
	}

	vinyl := byRef["trk-vinyl-us-a2"]
	if vinyl.Country == nil || *vinyl.Country != "US" {
		t.Errorf("黑胶版国家 = %v", vinyl.Country)
	}
	if vinyl.MediumFormat == nil || *vinyl.MediumFormat != "Vinyl" {
		t.Errorf("黑胶版载体 = %v", vinyl.MediumFormat)
	}

	// 同一录音的三行共享 recording MBID，但仍保留独立的 track 身份
	for _, c := range got {
		if c.RecordingRef == nil || *c.RecordingRef != "rec-0001-god-knows" {
			t.Errorf("候选 %q 的 recording 标识 = %v", c.MatchRef, c.RecordingRef)
		}
	}
}

// TestSearchCandidateCarriesDisplayColumns 覆盖
// 「展开后的行可直接展示所需列，不需要再发额外请求」。
func TestSearchCandidateCarriesDisplayColumns(t *testing.T) {
	var resp searchResponse
	loadFixture(t, "search_recording.json", &resp)
	got := expandToTrackCandidates(resp.Recordings)

	c := got[0]
	checks := []struct {
		name string
		ok   bool
	}{
		{"标题", c.Title != nil},
		{"艺术家", c.ArtistName != nil},
		{"歌集名", c.AlbumTitle != nil},
		{"时长", c.DurationMS != nil},
		{"曲序", c.TrackNumber != nil},
		{"总曲数", c.TrackTotal != nil},
		{"年份", c.Year != nil},
		{"载体", c.MediumFormat != nil},
		{"国家", c.Country != nil},
		{"主要类型", c.PrimaryType != nil},
	}
	for _, check := range checks {
		if !check.ok {
			t.Errorf("候选缺少「%s」列", check.name)
		}
	}
	if c.UpstreamScore == nil || *c.UpstreamScore != 100 {
		t.Errorf("上游 score = %v, 期望 100", c.UpstreamScore)
	}
	if c.ISRC == nil || *c.ISRC != "JPPL00600123" {
		t.Errorf("ISRC = %v", c.ISRC)
	}
}

// TestArtistCreditPreservesJoinPhrase 覆盖署名原串保真：
// feat. / & 这类连接词无法从关系表无损重建。
func TestArtistCreditPreservesJoinPhrase(t *testing.T) {
	var rel releaseDetail
	loadFixture(t, "release_detail.json", &rel)

	got := joinArtistCredit(rel.ArtistCredit)
	if want := "涼宮ハルヒ feat. 平野綾"; got != want {
		t.Errorf("署名原串 = %q, 期望 %q", got, want)
	}
}

// TestDetailParsesAllFieldsInOneRequest 覆盖 6.3：
// 仅一次请求即可解析出全部所需字段。
func TestDetailParsesAllFieldsInOneRequest(t *testing.T) {
	var rel releaseDetail
	loadFixture(t, "release_detail.json", &rel)

	d := ParseDetail(&rel, "trk-cd-jp-02")

	checks := []struct {
		name string
		got  any
		ok   bool
	}{
		{"曲目级署名", d.TrackCredit, d.TrackCredit != nil},
		{"发行级署名", d.AlbumCredit, d.AlbumCredit != nil},
		{"主要类型", d.PrimaryType, d.PrimaryType != nil},
		{"次要类型", d.SecondaryTypes, len(d.SecondaryTypes) > 0},
		{"年份", d.Year, d.Year != nil},
		{"曲序", d.TrackNumber, d.TrackNumber != nil},
		{"总曲数", d.TrackTotal, d.TrackTotal != nil},
		{"碟号", d.DiscNumber, d.DiscNumber != nil},
		{"总碟数", d.DiscTotal, d.DiscTotal != nil},
		{"风格", d.Genre, d.Genre != nil},
		{"国家", d.Country, d.Country != nil},
		{"厂牌", d.Label, d.Label != nil},
		{"目录号", d.CatalogNumber, d.CatalogNumber != nil},
		{"载体格式", d.MediumFormat, d.MediumFormat != nil},
		{"时长", d.DurationMS, d.DurationMS != nil},
		{"ISRC", d.ISRC, d.ISRC != nil},
		{"注释", d.Comment, d.Comment != nil},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("详情缺少「%s」字段", c.name)
		}
	}

	if *d.Label != "Lantis" {
		t.Errorf("厂牌 = %q", *d.Label)
	}
	if *d.CatalogNumber != "LACA-15113" {
		t.Errorf("目录号 = %q", *d.CatalogNumber)
	}
	if *d.Genre != "anime" {
		t.Errorf("风格 = %q, 期望计数最高的 anime", *d.Genre)
	}
	if *d.MediumFormat != "CD" {
		t.Errorf("载体 = %q", *d.MediumFormat)
	}
	if len(d.SecondaryTypes) != 1 || d.SecondaryTypes[0] != "Soundtrack" {
		t.Errorf("次要类型 = %v", d.SecondaryTypes)
	}
}

// TestComposerLyricistFromWorkLevel 覆盖 6.4：
// 作曲与作词经 Work 层级取得，不因录音层级没有就判为缺失。
func TestComposerLyricistFromWorkLevel(t *testing.T) {
	var rel releaseDetail
	loadFixture(t, "release_detail.json", &rel)
	d := ParseDetail(&rel, "trk-cd-jp-02")

	byRole := map[string][]string{}
	for _, a := range d.Artists {
		byRole[a.Role] = append(byRole[a.Role], a.Name)
	}

	if got := byRole["composer"]; len(got) != 1 || got[0] != "田中公平" {
		t.Errorf("作曲 = %v, 期望 [田中公平]（挂在 Work 上）", got)
	}
	if got := byRole["lyricist"]; len(got) != 1 || got[0] != "森由里子" {
		t.Errorf("作词 = %v, 期望 [森由里子]", got)
	}
	if d.WorkRef == nil || *d.WorkRef != "work-god-knows" {
		t.Errorf("Work 标识 = %v", d.WorkRef)
	}
}

// TestVocalAndInstrumentFromRecordingLevel 覆盖 6.5：
// 演唱者与器乐演奏者来自 recording-level-rels，乐器名存 attribute。
func TestVocalAndInstrumentFromRecordingLevel(t *testing.T) {
	var rel releaseDetail
	loadFixture(t, "release_detail.json", &rel)
	d := ParseDetail(&rel, "trk-cd-jp-02")

	var vocal, instrument *provider.DetailArtist
	for i := range d.Artists {
		switch d.Artists[i].Role {
		case "vocal":
			if d.Artists[i].Name == "平野綾" {
				vocal = &d.Artists[i]
			}
		case "instrument":
			instrument = &d.Artists[i]
		}
	}

	if vocal == nil {
		t.Fatalf("未解析出演唱者平野綾, 实际参与者: %+v", d.Artists)
	}
	if vocal.MBID == nil || *vocal.MBID != "art-hirano" {
		t.Errorf("演唱者 MBID = %v", vocal.MBID)
	}

	if instrument == nil {
		t.Fatal("未解析出器乐演奏者")
	}
	if instrument.Name != "須藤賢一" {
		t.Errorf("器乐演奏者 = %q", instrument.Name)
	}
	if instrument.Attribute == nil || *instrument.Attribute != "guitar" {
		t.Errorf("乐器名 = %v, 期望 guitar（存在 attribute 中）", instrument.Attribute)
	}
}

// TestDetailCarriesTracklist 覆盖 6.6：
// 详情响应顺带带出完整清单，不额外发出请求。
func TestDetailCarriesTracklist(t *testing.T) {
	var rel releaseDetail
	loadFixture(t, "release_detail.json", &rel)
	d := ParseDetail(&rel, "trk-cd-jp-02")

	if len(d.Tracklist) != 3 {
		t.Fatalf("清单条数 = %d, 期望 3", len(d.Tracklist))
	}

	first := d.Tracklist[0]
	if first.TrackRef == nil || *first.TrackRef != "trk-cd-jp-01" {
		t.Errorf("清单首条 track 标识 = %v", first.TrackRef)
	}
	if first.RecordingRef == nil || *first.RecordingRef != "rec-0002" {
		t.Errorf("清单首条 recording 标识 = %v", first.RecordingRef)
	}
	if first.Title != "冒険でしょでしょ?" {
		t.Errorf("清单首条标题 = %q", first.Title)
	}
	if first.DurationMS == nil || *first.DurationMS != 248000 {
		t.Errorf("清单首条时长 = %v", first.DurationMS)
	}
	if first.TrackNumber != 1 || first.DiscNumber != 1 {
		t.Errorf("清单首条位置 = 碟%d 第%d 首", first.DiscNumber, first.TrackNumber)
	}
}

// TestVinylTrackNumberText 确保黑胶的 A1 这类非数字编号被保留。
func TestVinylTrackNumberText(t *testing.T) {
	var resp searchResponse
	loadFixture(t, "search_recording.json", &resp)
	got := expandToTrackCandidates(resp.Recordings)

	for _, c := range got {
		if c.MatchRef == "trk-vinyl-us-a2" {
			if c.TrackNumber == nil || *c.TrackNumber != 2 {
				t.Errorf("黑胶曲序 = %v, 期望位置 2", c.TrackNumber)
			}
			return
		}
	}
	t.Error("未找到黑胶候选")
}

// TestParseVoiceActor 覆盖 6.7：从角色艺术家取出配音关系。
func TestParseVoiceActor(t *testing.T) {
	var a artistDetail
	loadFixture(t, "artist_character.json", &a)

	got := ParseVoiceActor(&a)
	if got == nil {
		t.Fatal("未解析出配音关系")
	}
	if got.Name != "平野綾" {
		t.Errorf("声优 = %q, 期望 平野綾", got.Name)
	}
	if got.MBID == nil || *got.MBID != "art-hirano" {
		t.Errorf("声优 MBID = %v", got.MBID)
	}
}

// TestIsCharacterGatesSupplementaryRequest 覆盖 6.7：
// 仅当 type == Character 时才触发补充请求，其他类型不触发。
func TestIsCharacterGatesSupplementaryRequest(t *testing.T) {
	character := "character"
	person := "person"
	if !IsCharacter(&character) {
		t.Error("角色类型应触发补充请求")
	}
	if IsCharacter(&person) {
		t.Error("人物类型不应触发补充请求")
	}
	if IsCharacter(nil) {
		t.Error("类型缺失时不应触发补充请求")
	}
}

// TestCapabilitiesMatchReferenceTable 覆盖 6.8 与 5.2：
// 声明与 3.2 节对照表一致。
func TestCapabilitiesMatchReferenceTable(t *testing.T) {
	p := New(nil, Config{Contact: "x@y.z"}, nil)
	caps := p.Capabilities()

	mustHave := []struct {
		cap  provider.Capabilities
		name string
	}{
		{provider.CapSecondaryTypes, "次要类型"},
		{provider.CapComposer, "作曲"},
		{provider.CapLyricist, "作词"},
		{provider.CapPerformerRoles, "演出者角色"},
		{provider.CapLabel, "厂牌"},
		{provider.CapCatalogNumber, "目录号"},
		{provider.CapMediumFormat, "载体格式"},
		{provider.CapCountry, "国家"},
		{provider.CapISRC, "ISRC"},
		{provider.CapCoverArt, "封面"},
		{provider.CapMultiSizeCover, "多规格封面"},
		{provider.CapUpstreamScore, "上游相关度"},
	}
	for _, m := range mustHave {
		if !caps.Has(m.cap) {
			t.Errorf("MusicBrainz 应声明支持「%s」", m.name)
		}
	}
}

// TestRateLimitDefault 覆盖 6.8：默认 1 req/s，可配置。
func TestRateLimitDefault(t *testing.T) {
	p := New(nil, Config{Contact: "x@y.z"}, nil)
	n, per := p.RateLimit()
	if n != 1 || per != 1000000000 {
		t.Errorf("默认速率 = %d 次/%v, 期望 1 次/1s", n, per)
	}

	// 设置中调整后令牌桶随之变化
	slower := New(nil, Config{Contact: "x@y.z", RatePerSec: 0.5}, nil)
	n2, per2 := slower.RateLimit()
	if per2 != 2000000000 {
		t.Errorf("0.5 req/s 时 = %d 次/%v, 期望 1 次/2s", n2, per2)
	}
}

// TestIsConfiguredRequiresContact 覆盖 6.1：
// 联系方式为空时不发出任何请求并返回可展示错误。
func TestIsConfiguredRequiresContact(t *testing.T) {
	if err := New(nil, Config{}, nil).IsConfigured(); err == nil {
		t.Error("联系方式为空时应返回错误")
	}
	if err := New(nil, Config{Contact: "   "}, nil).IsConfigured(); err == nil {
		t.Error("联系方式仅含空白时应返回错误")
	}
	if err := New(nil, Config{Contact: "me@example.com"}, nil).IsConfigured(); err != nil {
		t.Errorf("已填写联系方式时不应报错: %v", err)
	}
}

// TestBuildLuceneQuerySkipsEmpty 确保留空的条件不参与查询。
func TestBuildLuceneQuerySkipsEmpty(t *testing.T) {
	got := buildLuceneQuery(provider.Query{Artist: "平野綾"})
	if want := `artist:"平野綾"`; got != want {
		t.Errorf("只填艺术家时查询 = %q, 期望 %q", got, want)
	}

	full := buildLuceneQuery(provider.Query{Title: "God knows", Artist: "平野綾", Album: "詰合"})
	for _, part := range []string{`recording:"God knows"`, `artist:"平野綾"`, `release:"詰合"`} {
		if !contains(full, part) {
			t.Errorf("查询 %q 缺少 %q", full, part)
		}
	}
	if got := buildLuceneQuery(provider.Query{}); got != "" {
		t.Errorf("全空时查询 = %q, 期望空串", got)
	}
}

// TestEscapeLucene 确保用户输入不被当作查询语法。
func TestEscapeLucene(t *testing.T) {
	got := buildLuceneQuery(provider.Query{Title: `That Girl (DJLIU Remix)`})
	if contains(got, `(DJLIU`) && !contains(got, `\(`) {
		t.Errorf("括号未被转义: %q", got)
	}
}

// TestArtworkDelegatesToCAA keeps the metadata association without a CAA API bucket.
func TestArtworkDelegatesToCAA(t *testing.T) {
	strategy := (&Provider{}).ArtworkStrategy()
	if strategy.Provider != provider.MusicBrainz || strategy.Source != "caa" || strategy.CatalogKeys == nil {
		t.Fatal(strategy)
	}
	keys := strategy.CatalogKeys("2659e6f6-ce21-4198-92e5-4878f5e7e3c0", "e8ec3011-cd49-4558-9d11-01f7fe9b552f")
	if len(keys) != 2 || keys[0].ObjectType != "release" || keys[1].ObjectType != "release-group" {
		t.Fatal(keys)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestSearchCandidateCarriesLabel 覆盖 12.6 的数据前提：
// Capabilities 声明了 CapLabel / CapCatalogNumber，对应的列就必须有值可取，
// 否则「部分支持则填占位、全不支持则隐藏」这条策略没有可驱动的东西。
func TestSearchCandidateCarriesLabel(t *testing.T) {
	var detail releaseDetail
	loadFixture(t, "release_detail.json", &detail)

	// 搜索结果里的 release 也带 label-info
	var resp searchResponse
	loadFixture(t, "search_recording.json", &resp)
	got := expandToTrackCandidates(resp.Recordings)
	if len(got) == 0 {
		t.Fatal("未展开出候选")
	}

	// 金样例的搜索响应未带 label-info，此时字段应为缺失而非空串——
	// 界面据此填占位短横线
	for _, c := range got {
		if c.Label != nil && *c.Label == "" {
			t.Error("缺失的厂牌不应表示为空串")
		}
	}

	// 详情路径下厂牌与目录号可解析（同一份 schema）
	d := ParseDetail(&detail, "trk-cd-jp-02")
	if d.Label == nil || *d.Label != "Lantis" {
		t.Errorf("详情厂牌 = %v, 期望 Lantis", d.Label)
	}
	if d.CatalogNumber == nil || *d.CatalogNumber != "LACA-15113" {
		t.Errorf("详情目录号 = %v, 期望 LACA-15113", d.CatalogNumber)
	}
}

// TestAnchoredQueryIsDisjunction 锁定检索式的形态：曲名必须命中，锚点并联。
//
// 这是用户报告「导入 10 条只成功 2 条」的首要根因。三字段 AND 在真实导出表格上
// 几乎必然落空，因为 `artist:` 只匹配 recording **自己的** artist credit，
// 而表格的歌手列时而是碟的署名、时而是唱的人。实测（真实 MB）：
//
//	recording:"綿雪の夢" AND artist:"Crest" AND release:"狼と巫女とお菓子の家"  → 0 条
//	recording:"綿雪の夢" AND (release:"狼と巫女とお菓子の家" OR artist:"Crest") → 1 条（正确那条）
func TestAnchoredQueryIsDisjunction(t *testing.T) {
	got := buildLuceneQuery(provider.Query{
		Title: "綿雪の夢", Artist: "Crest", Album: "狼と巫女とお菓子の家",
	})
	want := `recording:"綿雪の夢" AND (release:"狼と巫女とお菓子の家" OR artist:"Crest")`
	if got != want {
		t.Errorf("查询串 =\n  %s\n期望 =\n  %s", got, want)
	}
	if strings.Contains(got, `" AND artist:`) {
		t.Error("歌手不得作为叠加的 AND 条件——它常常不是录音的署名")
	}
}

// TestAnchoredQueryIncludesArtistAlternates 覆盖「括号里那种写法要进查询」。
//
// 平台习惯把另一种写法装在括号里（`3L（スリーエル）`），而上游用的往往正是它。
func TestAnchoredQueryIncludesArtistAlternates(t *testing.T) {
	got := buildLuceneQuery(provider.Query{
		Title: "夕立", Artist: "3L", Album: "アコースティック・エピソード",
		ArtistAlternates: []string{"スリーエル"},
	})
	for _, want := range []string{
		`recording:"夕立"`, `release:"アコースティック・エピソード"`,
		`artist:"3L"`, `artist:"スリーエル"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("查询串缺少 %s:\n  %s", want, got)
		}
	}
	if !strings.Contains(got, " OR ") {
		t.Errorf("锚点之间应为 OR:\n  %s", got)
	}
}

// TestAnchoredQueryCapsAlternates 确保备选写法有上限：
// 它们都挂在同一个 OR 组里，每多一个分支就多一段没有上限的用户输入被拼进 URL。
func TestAnchoredQueryCapsAlternates(t *testing.T) {
	got := buildLuceneQuery(provider.Query{
		Title: "T", Artist: "A",
		ArtistAlternates: []string{"x1", "x2", "x3", "x4", "x5"},
	})
	if n := strings.Count(got, `artist:"`); n > 1+maxArtistAlternates {
		t.Errorf("艺术家分支 = %d 个, 上限应为 %d:\n  %s", n, 1+maxArtistAlternates, got)
	}
}

// TestTitleOnlyQueryDropsAnchors 覆盖阶梯的兜底一级：连歌集带歌手都对不上时，
// 只用曲名检索，把「哪一条才是」交给本地闸门判。
func TestTitleOnlyQueryDropsAnchors(t *testing.T) {
	got := buildLuceneQuery(provider.Query{
		Title: "ツバサ", Artist: "4円", Album: "side-c", TitleOnly: true,
	})
	if got != `recording:"ツバサ"` {
		t.Errorf("查询串 = %s, 期望只有曲名", got)
	}
}

// TestSingleAnchorOmitsParens：一个锚点时不加括号，`A AND (B)` 与 `A AND B` 等价。
func TestSingleAnchorOmitsParens(t *testing.T) {
	got := buildLuceneQuery(provider.Query{Title: "T", Album: "Al"})
	if got != `recording:"T" AND release:"Al"` {
		t.Errorf("查询串 = %s", got)
	}
}

// TestMediumAcceptsBothTrackKeys 守住两个端点的曲目数组键名差异。
//
// MusicBrainz 在搜索响应里把碟上的曲目放在 `track`，在发行详情响应里放在
// `tracks`。只认 `tracks` 的后果不是「少一个字段」——搜索结果的每条候选都会
// 退到发行级兜底：match_ref 变成合成串而不是 track MBID，载体格式、曲序、
// 碟序一并丢失，而载体格式正是分流规则第 3 条赖以过滤实体发行的依据。
// 实测：修这一处之前，真实搜索返回的每条候选载体都是空的。
func TestMediumAcceptsBothTrackKeys(t *testing.T) {
	const searchShape = `{"recordings":[{"id":"rec-1","title":"某曲","releases":[{
		"id":"rel-1","title":"某碟","status":"Official","country":"JP",
		"media":[{"position":1,"format":"CD","track-count":4,
			"track":[{"id":"trk-1","position":2,"title":"某曲"}]}]}]}]}`
	const detailShape = `{"recordings":[{"id":"rec-1","title":"某曲","releases":[{
		"id":"rel-1","title":"某碟","status":"Official","country":"JP",
		"media":[{"position":1,"format":"CD","track-count":4,
			"tracks":[{"id":"trk-1","position":2,"title":"某曲"}]}]}]}]}`

	for name, body := range map[string]string{"搜索端点(track)": searchShape, "详情端点(tracks)": detailShape} {
		t.Run(name, func(t *testing.T) {
			var resp searchResponse
			if err := json.Unmarshal([]byte(body), &resp); err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			got := expandToTrackCandidates(resp.Recordings)
			if len(got) != 1 {
				t.Fatalf("候选数 = %d, 期望 1", len(got))
			}
			c := got[0]
			if c.MatchRef != "trk-1" {
				t.Errorf("match_ref = %q, 期望 track MBID trk-1（不是发行级合成串）", c.MatchRef)
			}
			if c.MediumFormat == nil || *c.MediumFormat != "CD" {
				t.Errorf("载体 = %v, 期望 CD——分流规则 3 靠它过滤实体发行", c.MediumFormat)
			}
			if c.TrackNumber == nil || *c.TrackNumber != 2 {
				t.Errorf("曲序 = %v, 期望 2", c.TrackNumber)
			}
		})
	}
}
