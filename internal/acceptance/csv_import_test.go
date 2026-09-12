package acceptance

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// 这一组用例锁定一次真实的导入：用户那份 10 行的导出表格。
//
// 候选的形状不是编的——它们是拿真实的 MusicBrainz 跑一遍抄回来的
// （曲名/署名/歌集/recording MBID/release group MBID 都照原样）。
// 这样既不必在仓库里塞几百 KB 的响应 JSON，也不必联网，
// 而「引擎面对上游真实返回的东西会怎么判」这件事被完整地钉住了。
//
// 期望：7 条直接入库，3 条进复核。那 3 条（That Girl (DJLIU Remix) /
// Sacred Play SecretPlace_雨夜 / ツバサ）在 MusicBrainz 里确实没有。

// csvRow 是表格里的一行输入及其在上游的真实返回。
type csvRow struct {
	title, artist, album string
	candidates           []provider.Candidate
	wantImport           bool
	// wantTitle 是应当被选中的候选曲名（wantImport 为真时校验）。
	wantTitle string
	why       string
}

func mbCand(matchRef, title, artist, album, rec, rg string) provider.Candidate {
	c := provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: matchRef,
		Title: ptr(title), ArtistName: ptr(artist), AlbumTitle: ptr(album),
		ReleaseStatus: ptr("Official"), MediumFormat: ptr("CD"), Country: ptr("JP"),
	}
	if rec != "" {
		c.RecordingRef = ptr(rec)
	}
	if rg != "" {
		c.ReleaseGroupRef = ptr(rg)
	}
	return c
}

func csvRows() []csvRow {
	return []csvRow{
		{
			title: "綿雪の夢（繁雪之梦）", artist: "Crest", album: "狼と巫女とお菓子の家",
			candidates: []provider.Candidate{
				mbCand("t1", "綿雪の夢", "ハトリアヤ", "狼と巫女とお菓子の家", "rec-1", "rg-1"),
			},
			wantImport: true, wantTitle: "綿雪の夢",
			why: "歌手列写的是碟的署名（Crest），录音署名却是唱的人——靠歌集精确命中",
		},
		{
			title: "夕立（驟雨）", artist: "3L（スリーエル）", album: "アコースティック・エピソード",
			candidates: []provider.Candidate{
				mbCand("t2", "夕立", "CROW'SCLAW", "アコースティック・エピソード", "rec-2", "rg-2"),
			},
			wantImport: true, wantTitle: "夕立",
			why: "同上：歌手列是社团名，录音署名是另一个——靠歌集精确命中",
		},
		{
			title: "Tetra.", artist: "原田瞳（原田ひとみ）", album: "LINES",
			candidates: []provider.Candidate{
				mbCand("t3", "Tetra.", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~", "rec-3", "rg-3"),
				mbCand("t3b", "Tetra. (off vocal)", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~", "rec-3b", "rg-3"),
				mbCand("t3c", "Tetra. -movie ver.-", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~", "rec-3c", "rg-3"),
			},
			wantImport: true, wantTitle: "Tetra.",
			why: "括号里是另一种写法，署名精确；off vocal / movie ver. 必须被曲名闸挡掉",
		},
		{
			title: "世界中の雨（世间之雨）", artist: "チームドラゴン from AKB48",
			album: "心の羽根（ドラゴンボール改ver.）（心...",
			candidates: []provider.Candidate{
				mbCand("t4", "世界中の雨", "チームドラゴン from AKB48", "心の羽根", "rec-4", "rg-4"),
				mbCand("t4b", "世界中の雨 (off vocal ver.)", "チームドラゴン from AKB48", "心の羽根", "rec-4b", "rg-4"),
			},
			wantImport: true, wantTitle: "世界中の雨",
			why: "歌集列被导出截断（结尾省略号 + 半个左括号），署名精确兜住",
		},
		{
			title: "That Girl (DJLIU Remix)", artist: "Liu", album: "That Girl",
			candidates: nil,
			wantImport: false,
			why:        "MusicBrainz 里确实没有这个 remix；裸的 That Girl 不得冒充它",
		},
		{
			title: "you", artist: "茅野愛衣（かやの あい）", album: "冴えない彼女の育てかた 第4巻 特典C...",
			candidates: []provider.Candidate{
				mbCand("t6", "you(Vocal)", "霞ヶ丘詩羽(CV.茅野愛衣)", "冴えない彼女の育てかた 第4巻 特典CD", "rec-6", "rg-6a"),
				mbCand("t6b", "you(Vocal)", "霞ヶ丘詩羽(CV.茅野愛衣)", "冴えない彼女の育てかた ギャルゲーカバーソングコレクション", "rec-6", "rg-6b"),
			},
			wantImport: true, wantTitle: "you(Vocal)",
			why: "上游署名是角色 + CV，查询给的是声优；两条候选同一 recording，只是收录在两张碟",
		},
		{
			title: "3253212", artist: "reche", album: "3253212",
			candidates: []provider.Candidate{
				mbCand("t7", "3253212", "reche", "3253212", "rec-7", "rg-7a"),
				mbCand("t7b", "3253212", "reche", "reche GaLLERy#101 on live 2023", "rec-7b", "rg-7b"),
			},
			wantImport: true, wantTitle: "3253212",
			why: "单曲与 live 专辑跨发行组，但只有一条歌集精确——用户写的歌集把答案指了出来",
		},
		{
			title: "Sacred Play SecretPlace_雨夜", artist: "Wiz-H", album: "Sacred Play SecretPlace_雨夜",
			candidates: nil,
			wantImport: false,
			why:        "MusicBrainz 里确实没有",
		},
		{
			title: "By the Seaside (feat. Kitsui Akira)", artist: "Kirara Magic",
			album: "By the Seaside (feat. Kitsui Akira)",
			candidates: []provider.Candidate{
				mbCand("t9", "By the Seaside", "Kirara Magic feat. Kitsui Akira", "By the Seaside", "rec-9", "rg-9"),
				mbCand("t9b", "By the Seaside", "Lone Alpha & Shachimu", "By the Seaside", "rec-9b", "rg-9b"),
			},
			wantImport: true, wantTitle: "By the Seaside",
			why: "feat. 是参与者不是版本：录音名不含它，署名含它，两侧都要能对上",
		},
		{
			title: "ツバサ（羽翼）", artist: "4円（ヨエン）", album: "side-c",
			candidates: []provider.Candidate{
				mbCand("t10", "ツバサ", "BlooDye", "HAKU", "rec-10", "rg-10"),
				mbCand("t10b", "つばさ", "森下純菜", "Dream", "rec-10b", "rg-10b"),
			},
			wantImport: false,
			why:        "同名曲一大把，但没有一条的署名或歌集对得上——不能瞎选",
		},
	}
}

// csvSearcher 按曲名主干返回预设候选，模拟上游的子短语匹配。
type csvSearcher struct {
	rows []csvRow
	hits int
}

func (s *csvSearcher) SearchTracks(_ context.Context, q provider.Query) ([]provider.Candidate, error) {
	s.hits++
	for _, r := range s.rows {
		if matching.SearchCore(r.title) == q.Title || r.title == q.Title {
			return r.candidates, nil
		}
	}
	return nil, nil
}

// TestCSVImportEndToEnd 覆盖用户报告的那次导入：10 行里 7 行应直接入库。
//
// 修复前只有 2 行入库，其中 Tetra. 还选错了版本（选中了 off vocal）。
// 三处根因各有一条用例守着：
//
//	查询形态   三字段 AND → 锚定并联（1、2 两行的歌手列都不是录音署名）
//	曲名闸门   词元包含 → 版本标记必须一致（Tetra. 那三条）
//	署名判定   同一位艺术家的三种异写都要认（3、6、9 三行）
func TestCSVImportEndToEnd(t *testing.T) {
	engine := matching.NewEngine(config.Defaults().Matching)
	rows := csvRows()
	searcher := &csvSearcher{rows: rows}

	var imported, review int
	for _, row := range rows {
		t.Run(row.title, func(t *testing.T) {
			res, err := engine.Run(context.Background(), searcher, nil, matching.Input{
				Title: row.title, Artist: row.artist, Album: row.album,
			})
			if err != nil {
				t.Fatalf("匹配出错: %v", err)
			}

			gotImport := res.Decision.Outcome == matching.OutcomeImportUnique ||
				res.Decision.Outcome == matching.OutcomeImportLadder
			if gotImport != row.wantImport {
				t.Fatalf("分流 = %q, 期望入库=%v\n  理由: %s\n  候选 %d 条",
					res.Decision.Outcome, row.wantImport, row.why, len(res.Scored))
			}
			if row.wantImport {
				if got := deref(res.Decision.Selected.Title); got != row.wantTitle {
					t.Errorf("选中曲名 = %q, 期望 %q\n  理由: %s", got, row.wantTitle, row.why)
				}
			}
		})

		res, _ := engine.Run(context.Background(), searcher, nil, matching.Input{
			Title: row.title, Artist: row.artist, Album: row.album,
		})
		if res.Decision.Outcome == matching.OutcomeImportUnique ||
			res.Decision.Outcome == matching.OutcomeImportLadder {
			imported++
		} else {
			review++
		}
	}

	if imported != 7 || review != 3 {
		t.Errorf("入库 %d / 复核 %d, 期望 7 / 3", imported, review)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
