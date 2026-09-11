package acceptance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

// TestOfflineReplayFromProviderCache 覆盖 22.10：
// 解析器可在断网情况下用 provider_cache 的原始响应重放。
//
// 这是「先落盘后解析」的兑现：解析器是本项目最容易出错的部分，
// 而复现一次 bug 在 1 req/s 下代价高昂，落盘后可离线反复重放。
func TestOfflineReplayFromProviderCache(t *testing.T) {
	s := openStore(t)

	// 把金样例响应当作「此前落盘的原始响应」写入缓存
	fixtures := map[string]string{
		"/ws/2/recording":       "search_recording.json",
		"/ws/2/release/rel-cd-jp": "release_detail.json",
		"/ws/2/artist/art-haruhi": "artist_character.json",
	}
	hashes := map[string]string{}

	for endpoint, name := range fixtures {
		path := filepath.Join("..", "..", "testdata", "golden", "responses", "musicbrainz", name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取金样例 %s 失败: %v", name, err)
		}
		params := map[string]string{"fmt": "json", "endpoint": endpoint}
		hash := store.ParamsHash(params)
		hashes[endpoint] = hash
		// MB 数据为 CC0，TTL 为 0 表示不过期
		if err := s.PutCache("musicbrainz", endpoint, hash, body, 0); err != nil {
			t.Fatalf("落盘 %s 失败: %v", endpoint, err)
		}
	}

	// 断网重放：不发出任何请求，直接从缓存读原始响应再解析
	t.Run("重放搜索响应", func(t *testing.T) {
		entry, err := s.GetCacheRaw("musicbrainz", "/ws/2/recording", hashes["/ws/2/recording"])
		if err != nil {
			t.Fatalf("读取缓存失败: %v", err)
		}
		var parsed struct {
			Recordings []struct {
				ID       string `json:"id"`
				Title    string `json:"title"`
				Releases []struct {
					ID    string `json:"id"`
					Media []struct {
						Tracks []struct {
							ID string `json:"id"`
						} `json:"tracks"`
					} `json:"media"`
				} `json:"releases"`
			} `json:"recordings"`
		}
		if err := json.Unmarshal(entry.Body, &parsed); err != nil {
			t.Fatalf("离线解析失败: %v", err)
		}
		if len(parsed.Recordings) != 1 {
			t.Fatalf("录音数 = %d", len(parsed.Recordings))
		}
		// 一条录音收录于 3 张发行 → track 粒度展开为 3 行
		if len(parsed.Recordings[0].Releases) != 3 {
			t.Errorf("发行数 = %d, 期望 3", len(parsed.Recordings[0].Releases))
		}
	})

	t.Run("重放详情响应并解析", func(t *testing.T) {
		entry, err := s.GetCacheRaw("musicbrainz",
			"/ws/2/release/rel-cd-jp", hashes["/ws/2/release/rel-cd-jp"])
		if err != nil {
			t.Fatalf("读取缓存失败: %v", err)
		}

		// 用导出的解析入口重放——与在线路径是同一份代码
		detail, err := musicbrainz.ParseDetailJSON(entry.Body, "trk-cd-jp-02")
		if err != nil {
			t.Fatalf("离线解析详情失败: %v", err)
		}
		if detail.Label == nil || *detail.Label != "Lantis" {
			t.Errorf("厂牌 = %v", detail.Label)
		}
		// 作曲作词挂在 Work 上，离线重放同样要能解析出来
		var composer, lyricist bool
		for _, a := range detail.Artists {
			if a.Role == "composer" && a.Name == "田中公平" {
				composer = true
			}
			if a.Role == "lyricist" && a.Name == "森由里子" {
				lyricist = true
			}
		}
		if !composer || !lyricist {
			t.Errorf("离线重放未解析出作曲/作词: %+v", detail.Artists)
		}
		// 清单一并带出
		if len(detail.Tracklist) != 3 {
			t.Errorf("清单 = %d 条, 期望 3", len(detail.Tracklist))
		}
	})

	t.Run("重放角色响应", func(t *testing.T) {
		entry, err := s.GetCacheRaw("musicbrainz",
			"/ws/2/artist/art-haruhi", hashes["/ws/2/artist/art-haruhi"])
		if err != nil {
			t.Fatalf("读取缓存失败: %v", err)
		}
		va, err := musicbrainz.ParseVoiceActorJSON(entry.Body)
		if err != nil {
			t.Fatalf("离线解析失败: %v", err)
		}
		if va == nil || va.Name != "平野綾" {
			t.Errorf("配音关系 = %+v", va)
		}
	})

	// 过期的缓存对正常读取不可见，但离线重放仍可取到原始响应
	t.Run("过期缓存仍可重放", func(t *testing.T) {
		params := map[string]string{"id": "1"}
		hash := store.ParamsHash(params)
		s.PutCache("itunes", "/lookup", hash, []byte(`{"resultCount":0,"results":[]}`), time.Hour)
		s.DB().Exec(`UPDATE provider_cache SET fetched_at = ? WHERE provider = 'itunes'`,
			time.Now().Add(-2*time.Hour))

		if _, err := s.GetCache("itunes", "/lookup", hash); err == nil {
			t.Error("过期缓存不应被正常读取命中")
		}
		if _, err := s.GetCacheRaw("itunes", "/lookup", hash); err != nil {
			t.Errorf("过期缓存仍应可用于离线重放: %v", err)
		}
	})
}
