package store

import (
	"errors"
	"testing"
	"time"
)

// TestCacheRoundTrip 覆盖 2.9：按键读写与过期判定。
func TestCacheRoundTrip(t *testing.T) {
	s := openTestStore(t)

	params := map[string]string{"query": "recording:\"God knows\"", "limit": "25"}
	hash := ParamsHash(params)
	body := []byte(`{"recordings":[]}`)

	if err := s.PutCache("musicbrainz", "/ws/2/recording", hash, body, time.Hour); err != nil {
		t.Fatalf("写入缓存失败: %v", err)
	}

	entry, err := s.GetCache("musicbrainz", "/ws/2/recording", hash)
	if err != nil {
		t.Fatalf("读取缓存失败: %v", err)
	}
	if string(entry.Body) != string(body) {
		t.Errorf("缓存内容 = %q, 期望 %q", entry.Body, body)
	}

	// 不同参数是不同的键
	other := ParamsHash(map[string]string{"query": "recording:\"別の曲\"", "limit": "25"})
	if _, err := s.GetCache("musicbrainz", "/ws/2/recording", other); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("不同参数应未命中, 得到 %v", err)
	}
	// 不同 provider 的同一端点参数也是不同的键
	if _, err := s.GetCache("itunes", "/ws/2/recording", hash); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("不同 provider 应未命中, 得到 %v", err)
	}
}

// TestParamsHashIsOrderIndependent 确保同一组参数无论构造顺序都得到同一个键。
func TestParamsHashIsOrderIndependent(t *testing.T) {
	a := ParamsHash(map[string]string{"term": "God knows", "country": "jp", "limit": "25"})
	b := ParamsHash(map[string]string{"limit": "25", "country": "jp", "term": "God knows"})
	if a != b {
		t.Errorf("同一组参数的哈希应相同: %s vs %s", a, b)
	}
	c := ParamsHash(map[string]string{"term": "God knows", "country": "us", "limit": "25"})
	if a == c {
		t.Error("不同参数的哈希不应相同")
	}
}

// TestCacheExpiry 覆盖 iTunes 数据的缓存有效期约束：过期即视为不存在。
func TestCacheExpiry(t *testing.T) {
	s := openTestStore(t)
	hash := ParamsHash(map[string]string{"id": "1"})

	if err := s.PutCache("itunes", "/lookup", hash, []byte(`{}`), time.Hour); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	// 把获取时间改为两小时前，使其超过 1 小时的有效期
	if _, err := s.DB().Exec(
		`UPDATE provider_cache SET fetched_at = ? WHERE provider='itunes'`,
		time.Now().UTC().Add(-2*time.Hour)); err != nil {
		t.Fatalf("改写获取时间失败: %v", err)
	}

	if _, err := s.GetCache("itunes", "/lookup", hash); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("过期缓存应未命中, 得到 %v", err)
	}

	// 但原始响应仍可离线重放，用于复现解析问题
	raw, err := s.GetCacheRaw("itunes", "/lookup", hash)
	if err != nil {
		t.Fatalf("离线重放读取失败: %v", err)
	}
	if string(raw.Body) != `{}` {
		t.Errorf("重放内容 = %q", raw.Body)
	}
}

// TestCacheNoTTLNeverExpires 覆盖 MusicBrainz 数据长期留存：TTL 为 0 表示不过期。
func TestCacheNoTTLNeverExpires(t *testing.T) {
	s := openTestStore(t)
	hash := ParamsHash(map[string]string{"mbid": "abc"})

	if err := s.PutCache("musicbrainz", "/ws/2/release", hash, []byte(`{"id":"abc"}`), 0); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if _, err := s.DB().Exec(
		`UPDATE provider_cache SET fetched_at = ?`, time.Now().UTC().AddDate(-1, 0, 0)); err != nil {
		t.Fatalf("改写获取时间失败: %v", err)
	}

	if _, err := s.GetCache("musicbrainz", "/ws/2/release", hash); err != nil {
		t.Errorf("一年前的 MB 缓存不应过期, 得到 %v", err)
	}
}

// TestClearCache 覆盖设置页的清除缓存动作。
func TestClearCache(t *testing.T) {
	s := openTestStore(t)
	hash := ParamsHash(map[string]string{"a": "1"})
	s.PutCache("musicbrainz", "/e", hash, []byte("x"), time.Hour)

	if err := s.ClearCache(); err != nil {
		t.Fatalf("清除缓存失败: %v", err)
	}
	if _, err := s.GetCache("musicbrainz", "/e", hash); !errors.Is(err, ErrCacheMiss) {
		t.Errorf("清除后应未命中, 得到 %v", err)
	}
}
