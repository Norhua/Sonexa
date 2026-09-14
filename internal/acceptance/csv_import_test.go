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
// 候选不是编的——它们是拿真实的 MusicBrainz 跑一遍**原样**抄回来的
// （track MBID / 曲名 / 署名 / 歌集 / recording MBID / release group MBID /
// 发行状态 / 载体 / 国家 都照原样，见 musicbrainz.TestLiveDump）。
// 这样既不必在仓库里塞几百 KB 的响应 JSON，也不必联网，
// 而「引擎面对上游真实返回的东西会怎么判」这件事被完整地钉住了。
//
// 上一版夹具每行只留了一条发行，那正是 `世界中の雨` 那个 bug 藏身的地方：
// 真实返回里同一条录音挂在 11 张同名 CD 发行上，夹具把它简化成 1 张，
// 于是「同组多条 → 该不该自动裁决」这个分支从来没有被这组用例走到过。
// 夹具**不再做这种简化**。

// csvRow 是表格里的一行输入及其在上游的真实返回。
type csvRow struct {
	title, artist, album string
	candidates           []provider.Candidate
	// wantOutcome 是期望的分流结果。
	wantOutcome matching.Outcome
	// wantTitle 是应当被选中的候选曲名（入库时校验）。
	wantTitle string
	// wantScored 是应当过闸、进入候选池的条数（复核队列里看到的就是它们）。
	wantScored int
	why        string
}

// mbCand 按真实返回构造一条候选。空串表示上游没给这个字段。
func mbCand(matchRef, title, artist, album, rec, rg, status, format, country string) provider.Candidate {
	c := provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: matchRef,
		Title: ptr(title), ArtistName: ptr(artist), AlbumTitle: ptr(album),
	}
	for _, f := range []struct {
		val string
		dst **string
	}{
		{rec, &c.RecordingRef}, {rg, &c.ReleaseGroupRef},
		{status, &c.ReleaseStatus}, {format, &c.MediumFormat}, {country, &c.Country},
	} {
		if f.val != "" {
			*f.dst = ptr(f.val)
		}
	}
	return c
}

func csvRows() []csvRow {
	return []csvRow{
		{
			title: "綿雪の夢（繁雪之梦）", artist: "Crest",
			album: "狼と巫女とお菓子の家",
			candidates: []provider.Candidate{
				mbCand("3d9c04e3-2f21-4e1f-a307-e40a96ff82cf", "綿雪の夢", "ハトリアヤ", "狼と巫女とお菓子の家",
					"7f4f192d-efb5-4d52-9005-74a435b1fd00", "b5fca0ba-12a4-413a-aabc-d026ccefe47f", "Official", "CD", "JP"),
			},
			wantOutcome: matching.OutcomeImportUnique,
			wantTitle:   "綿雪の夢",
			wantScored:  1,
			why:         "歌手列写的是碟的署名（Crest），录音署名却是唱的人——靠歌集精确命中，全场唯一高置信",
		},
		{
			title: "夕立（驟雨）", artist: "3L（スリーエル）",
			album: "アコースティック・エピソード",
			candidates: []provider.Candidate{
				mbCand("43c5bf0e-3b4f-445e-a6d8-4c7eda045e34", "Yuudachi", "CROW'SCLAW", "Acoustic Episode",
					"35be3e58-8f8b-48bc-b5f4-b930c91cfa6e", "2dc07f35-f053-484a-8b9b-6c407fffc3ed", "Pseudo-Release", "CD", ""),
				mbCand("adfd4998-ed0e-3297-9e57-df1275caa0cc", "夕立", "CROW'SCLAW", "アコースティック・エピソード",
					"35be3e58-8f8b-48bc-b5f4-b930c91cfa6e", "2dc07f35-f053-484a-8b9b-6c407fffc3ed", "Official", "CD", "JP"),
			},
			wantOutcome: matching.OutcomeImportUnique,
			wantTitle:   "夕立",
			wantScored:  1, // 罗马字那条被曲名闸挡掉
			why:         "同上：歌手列是社团名。罗马字那条是 Pseudo-Release 的转写曲名，被曲名闸挡掉",
		},
		{
			title: "Tetra.", artist: "原田瞳（原田ひとみ）",
			album: "LINES",
			candidates: []provider.Candidate{
				mbCand("00051533-51c9-4398-8106-c2183cc333de", "Tetra.", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~",
					"915427e8-9d01-470b-90cc-4acffe481425", "e8ec3011-cd49-4558-9d11-01f7fe9b552f", "Official", "CD", "JP"),
				mbCand("ad13066e-36ce-4ca9-96c5-63e22c5e337c", "Tetra. (off vocal)", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~",
					"d0e36834-cb8c-4b1d-8a71-2a91d6b4328a", "e8ec3011-cd49-4558-9d11-01f7fe9b552f", "Official", "CD", "JP"),
				mbCand("d635bb70-6ff1-4c15-aa98-07d9a6f958ec", "Tetra. -movie ver.-", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~",
					"b280f26a-1f16-473e-a674-5e33397b0b0f", "e8ec3011-cd49-4558-9d11-01f7fe9b552f", "Official", "CD", "JP"),
				mbCand("1ff25716-0039-43bd-a75e-4900ab15b528", "Tetra. -movie ver.- (off vocal)", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~",
					"1299366a-17f4-4cb4-bf0f-f81cd0c76130", "e8ec3011-cd49-4558-9d11-01f7fe9b552f", "Official", "CD", "JP"),
			},
			wantOutcome: matching.OutcomeImportUnique,
			wantTitle:   "Tetra.",
			wantScored:  1, // off vocal / movie ver. 三条被曲名闸挡掉
			why:         "括号里是另一种写法，署名精确；off vocal / movie ver. 三条必须被曲名闸挡掉",
		},
		{
			title: "世界中の雨（世间之雨）", artist: "チームドラゴン from AKB48",
			album: "心の羽根（ドラゴンボール改ver.）（心...",
			candidates: []provider.Candidate{
				// 真实返回 22 条（同一录音 11 张同名 CD 发行 × 2 条录音）。
				// 这里留 11 + 2：11 张发行是判定的全部依据，off vocal 留两条够证明曲名闸生效。
				mbCand("5d444cdf-25d7-3177-81aa-07f39d46af6f", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("146fd64b-3fa5-338e-9853-51b7a1939466", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("355e93ba-2c26-3642-8f76-82d8cf417495", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("332fbd25-caaa-3a88-aa93-4750e253eaf3", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("fbcc8d05-0237-33c0-a8ff-136c7accde11", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("50924f6f-6795-3932-8558-ed6c600d37b7", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("5e5f141f-cdaa-3346-9f9a-43b572006e9b", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("6e862e7e-d4a2-3c3e-bdc1-95fae9568f41", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("7b6ec98d-1ebf-377e-ab95-1a2a7e0cf9b1", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("cd25916d-28be-3e75-b8fa-6ca6fd5d6dd7", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("a98c6566-cbdc-36f2-84f1-0a50b140844e", "世界中の雨", "チームドラゴン from AKB48", "心の羽根",
					"fea2d6f4-0957-4090-baba-b013ae3dfbcd", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("adaa9d55-a3ec-388f-b3c7-8870ed44a628", "世界中の雨 (off vocal ver.)", "チームドラゴン from AKB48", "心の羽根",
					"6789cb93-5b4d-4156-a29a-921ff32a9281", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
				mbCand("902b471c-52f6-3cb6-9bd5-3ea600469eb2", "世界中の雨 (off vocal ver.)", "チームドラゴン from AKB48", "心の羽根",
					"6789cb93-5b4d-4156-a29a-921ff32a9281", "9b793f9f-12fd-45f6-9e2e-50ad4ac7389d", "Official", "CD", "JP"),
			},
			wantOutcome: matching.OutcomeReviewManyReleases,
			wantScored:  11, // off vocal 两条被曲名闸挡掉，11 张同名 CD 全部过闸
			why: "用户报告的那条：同一条录音挂在 11 张同名 CD 发行上，过滤实体载体后仍剩 11 条，" +
				"没有任何客观依据能分出高下——不能替用户挑一个",
		},
		{
			title: "That Girl (DJLIU Remix)", artist: "Liu",
			album:       "That Girl",
			candidates:  nil,
			wantOutcome: matching.OutcomeReviewNoMatch,
			wantScored:  0,
			why:         "MusicBrainz 里确实没有这个 remix；裸的 That Girl 不得冒充它",
		},
		{
			title: "you", artist: "茅野愛衣（かやの あい）",
			album: "冴えない彼女の育てかた 第4巻 特典C...",
			candidates: []provider.Candidate{
				mbCand("f4458acc-aa40-40b0-a0b7-aaa27df24b04", "you(Vocal)", "霞ヶ丘詩羽(CV.茅野愛衣)", "冴えない彼女の育てかた ギャルゲーカバーソングコレクション",
					"2283ceda-78b1-4d20-8f52-bdf73bd53686", "83ea5ea3-4b39-4b49-b749-59a20f8a92a5", "Official", "CD", "JP"),
				mbCand("f0a33d1a-628a-4a53-bc4e-6af077505956", "(Vocal) you", "霞ヶ丘詩羽(CV.茅野愛衣)", "冴えない彼女の育てかた 第4巻 特典CD",
					"2283ceda-78b1-4d20-8f52-bdf73bd53686", "c085f961-892b-4232-bc6f-267e72703ef7", "Official", "CD", "JP"),
			},
			wantOutcome: matching.OutcomeReviewCrossGroup,
			wantScored:  2,
			why:         "两条候选虽是同一条 recording，却分属两个发行组——规则 4 没有例外，交人工",
		},
		{
			title: "3253212", artist: "reche",
			album: "3253212",
			candidates: []provider.Candidate{
				mbCand("710586d7-fa8a-4384-bdc4-6e80c5170dfa", "3253212", "reche", "3253212",
					"9fd44099-c359-40bc-b3db-a6884b39f240", "893ac86f-41b2-4577-90e3-4727d02b5a27", "Official", "Digital Media", "XW"),
				mbCand("97d35efa-837f-4587-b3bc-324c301cd80f", "3253212", "reche", "reche GaLLERy#101 on live 20230407 in Zepp DiverCity TOKYO",
					"9d2cc5b8-89f2-4c28-a9d3-c2182052893e", "5758dc80-fd8b-4e15-b1ab-7202a21137a1", "Official", "Blu-ray", "JP"),
				mbCand("2b85b4ca-340b-408c-accd-a2dac46a425b", "3253212", "reche", "reche GaLLERy#101 on live 20230407 in Zepp DiverCity TOKYO",
					"9d2cc5b8-89f2-4c28-a9d3-c2182052893e", "5758dc80-fd8b-4e15-b1ab-7202a21137a1", "Official", "Blu-ray", "JP"),
			},
			wantOutcome: matching.OutcomeReviewCrossGroup,
			wantScored:  3, // 署名都是 reche，三条都有旁证
			why:         "同名单曲与 live 蓝光跨发行组。只有单曲那条歌集精确，但那也不足以替用户裁决",
		},
		{
			title: "Sacred Play SecretPlace_雨夜", artist: "Wiz-H",
			album:       "Sacred Play SecretPlace_雨夜",
			candidates:  nil,
			wantOutcome: matching.OutcomeReviewNoMatch,
			wantScored:  0,
			why:         "MusicBrainz 里确实没有",
		},
		{
			title: "By the Seaside (feat. Kitsui Akira)", artist: "Kirara Magic",
			album: "By the Seaside (feat. Kitsui Akira)",
			candidates: []provider.Candidate{
				mbCand("2b958ece-3301-4953-9a52-b6fd1847566b", "By the Seaside", "Kirara Magic feat. Kitsui Akira", "By the Seaside",
					"486b3de3-da12-416a-811d-e1dd34294b26", "76e897c4-3ed8-48f6-a6d5-ea6dd7f7ce37", "Official", "Digital Media", "XW"),
				mbCand("9ba74ce0-0fce-40f2-ab73-2682b3eece72", "By the Seaside", "Lone Alpha & Shachimu", "By the Seaside",
					"774a31bc-c47f-4fa9-9c07-bfd2179c38a7", "bb9a3cc0-75ff-4f7c-a50a-54a724465e73", "Official", "Digital Media", "XW"),
				mbCand("343931f4-302a-4a53-8fa8-a329419f8e96", "By the Seaside", "Austin Farwell", "By the Seaside",
					"93b1122b-732d-415b-82d6-88ec2705e861", "83b47131-8bbd-414b-90ca-ad890fb3724c", "Official", "Digital Media", "XW"),
				mbCand("65f37565-d808-4737-b526-6177851a0ef4", "By the Seaside", "Lotus Beats", "By the Seaside",
					"81f2446c-7c0a-4863-a125-054aeac51403", "eeba808f-3656-4558-bde8-2b7838eea5fa", "Official", "Digital Media", "XW"),
				mbCand("5b8a21ae-6c26-4f1a-8926-31b04d58e779", "By the Seaside", "Lotus Beats", "Moontape 17",
					"81f2446c-7c0a-4863-a125-054aeac51403", "ae418cc6-8218-4d65-bfd0-61b551fecac8", "Official", "Digital Media", "XW"),
			},
			wantOutcome: matching.OutcomeImportUnique,
			wantTitle:   "By the Seaside",
			// 4 条：三条歌集同名（松档旁证）＋ 署名对得上那条；
			// `Moontape 17` 那条署名与歌集都对不上，落闸
			wantScored: 4,
			why: "feat. 是参与者不是版本：5 条同名曲都过了曲名闸，但只有这条的署名对得上 →" +
				"唯一高置信，走规则 2，不经实体载体过滤（它们全是 Digital Media）",
		},
		{
			title: "ツバサ（羽翼）", artist: "4円（ヨエン）",
			album: "side-c",
			candidates: []provider.Candidate{
				// 真实返回 43 条同名曲，留 6 条代表——没有一条的署名或歌集对得上。
				mbCand("2ae00de8-ee13-3d9f-b8bf-4a9acbb48f71", "つばさ", "熊木杏里", "君の名前",
					"5610cf7b-4569-49d8-9e56-ff8927d4b532", "dab961f3-de89-47d5-8db8-abc6efac2071", "Official", "CD", "JP"),
				mbCand("3c0c99d7-93b1-487b-afb3-c9e2e2038a1e", "つばさ", "渕崎ゆり子(李紅蘭)", "サクラ大戦 太正歌謡 カラオケ大全集",
					"4631fea0-4d34-4256-9f39-2858c2ce364a", "4f7bbdc3-a2d4-4e93-bd29-6fe17bbc3b30", "Official", "CD", "JP"),
				mbCand("6e55f9c1-a655-4853-9064-c455bcd86de9", "ツバサ", "美咲(CV：新田恵海)", "TVアニメ「18if」主題歌集",
					"545a88e0-84a5-4405-8455-07d0de5a0050", "b92016ee-a097-42e6-ad39-d04edae83ed4", "Official", "CD", "JP"),
				mbCand("25cc48a2-8849-4681-a0e7-9fc801b462ba", "ツバサ", "美咲(CV：新田恵海)", "TVアニメ「18if」主題歌集",
					"545a88e0-84a5-4405-8455-07d0de5a0050", "b92016ee-a097-42e6-ad39-d04edae83ed4", "Official", "Digital Media", "JP"),
				mbCand("3538a3bc-7ca5-4650-abe9-6a6643345aab", "ツバサ", "アンダーグラフ", "UNDER GRAPH",
					"ec4e44a2-b52d-4128-b252-6921c737835c", "3fb09397-8201-4bf0-8d8a-b3889a4fedd2", "Official", "Digital Media", "XW"),
				mbCand("900b2f02-e1bf-4772-80eb-951410bf7144", "つばさ", "歌心りえ", "SONGS",
					"8cd626d6-9a8c-4157-bb26-ce085e223703", "b0eac7b8-d93e-49fa-96dd-71e1bc9a0c62", "Official", "Digital Media", ""),
			},
			wantOutcome: matching.OutcomeReviewNoMatch,
			wantScored:  0,
			why: "同名曲一大把，但没有一条的署名或歌集对得上——一条旁证都没有的候选不该摆到" +
				"用户面前（用户报告：搜 4円 的『羽翼』本该无结果），因此候选池是空的",
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

// TestCSVImportEndToEnd 覆盖用户报告的那次导入：10 行输入各自该落到哪里。
//
// 分流规则没有例外，这组用例把每一条规则各钉住至少一行：
//
//	规则 2  唯一高置信          綿雪の夢 / 夕立 / Tetra. / By the Seaside
//	规则 3  同组多份发行        世界中の雨（11 张同名 CD，过滤后仍剩多条 → 复核）
//	规则 4  跨发行组            you / 3253212
//	规则 5  无结果 / 全低置信    That Girl / Sacred Play / ツバサ
//
// 期望：4 条直接入库，3 条进入复核，3 条未匹配。
func TestCSVImportEndToEnd(t *testing.T) {
	engine := matching.NewEngine(config.Defaults().Matching)
	rows := csvRows()
	searcher := &csvSearcher{rows: rows}

	var imported, review, unmatched int
	for _, row := range rows {
		t.Run(row.title, func(t *testing.T) {
			res, err := engine.Run(context.Background(), searcher, nil, matching.Input{
				Title: row.title, Artist: row.artist, Album: row.album,
			})
			if err != nil {
				t.Fatalf("匹配出错: %v", err)
			}

			if len(res.Scored) != row.wantScored {
				t.Errorf("过闸候选 %d 条, 期望 %d 条\n  理由: %s", len(res.Scored), row.wantScored, row.why)
			}
			if res.Decision.Outcome != row.wantOutcome {
				t.Fatalf("分流 = %q, 期望 %q\n  理由: %s\n  过闸候选 %d 条，高置信 %d 条",
					res.Decision.Outcome, row.wantOutcome, row.why,
					len(res.Scored), len(res.Decision.HighConfidence))
			}
			if row.wantTitle == "" {
				if res.Decision.Selected != nil {
					t.Errorf("不该自动选择, 却选中了 %q\n  理由: %s",
						deref(res.Decision.Selected.Title), row.why)
				}
				return
			}
			if res.Decision.Selected == nil {
				t.Fatalf("应选出一条, 理由: %s", row.why)
			}
			if got := deref(res.Decision.Selected.Title); got != row.wantTitle {
				t.Errorf("选中曲名 = %q, 期望 %q\n  理由: %s", got, row.wantTitle, row.why)
			}
		})

		switch row.wantOutcome {
		case matching.OutcomeImportUnique, matching.OutcomeImportLadder:
			imported++
		case matching.OutcomeReviewNoMatch:
			unmatched++
		default:
			review++
		}
	}

	if imported != 4 || review != 3 || unmatched != 3 {
		t.Errorf("入库 %d / 进入复核 %d / 未匹配 %d, 期望 4 / 3 / 3", imported, review, unmatched)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
