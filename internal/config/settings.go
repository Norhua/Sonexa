package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/norhua/sonexa/internal/provider"
)

// VariantStrategy 是查询变体的执行策略。
type VariantStrategy string

const (
	VariantProgressive VariantStrategy = "progressive" // 渐进式：首个变体高置信即停（默认）
	VariantExhaustive  VariantStrategy = "exhaustive"  // 全量：每条都执行全部变体
)

// TracklistFetchTiming 是曲目清单的拉取时机。
type TracklistFetchTiming string

const (
	TracklistOnAlbumOpen TracklistFetchTiming = "on_album_open" // 首次打开专辑时（默认）
	TracklistOnMatch     TracklistFetchTiming = "on_match"      // 匹配时即拉取
)

// GeneralSettings 对应设置页的「常规」分组。
type GeneralSettings struct {
	Theme    string `json:"theme"`    // dark | light
	Language string `json:"language"` // zh-CN
}

// MusicBrainzSettings 对应 MusicBrainz 的配置分组。
type MusicBrainzSettings struct {
	// Contact 是 User-Agent 中的联系方式，必填；未填写时全局禁用出网。
	Contact    string  `json:"contact"`
	ServerURL  string  `json:"serverUrl"`
	RatePerSec float64 `json:"ratePerSec"`
}

// ITunesSettings 对应 iTunes 的配置分组。
type ITunesSettings struct {
	DefaultCountry string  `json:"defaultCountry"` // 空表示跟随系统 locale
	RatePerMin     float64 `json:"ratePerMin"`
}

// QQMusicSettings 对应 QQ 音乐的配置分组。
type QQMusicSettings struct {
	RatePerMin float64 `json:"ratePerMin"`
}

// MatchingSettings 对应设置页的「匹配」分组。
type MatchingSettings struct {
	TitleWeight        float64 `json:"titleWeight"`
	CreditExactWeight  float64 `json:"creditExactWeight"`
	CreditFuzzyWeight  float64 `json:"creditFuzzyWeight"`
	AlbumExactWeight   float64 `json:"albumExactWeight"`
	AlbumFuzzyWeight   float64 `json:"albumFuzzyWeight"`
	UpstreamScoreScale float64 `json:"upstreamScoreScale"`
	UpstreamScoreCap   float64 `json:"upstreamScoreCap"`
	// StripPenalty 已不再被读取，保留字段只为不让既有 settings.json 在
	// 反序列化时丢字段。剥离变体曾经靠扣分压制误剥离，现在曲名是否对得上
	// 由 matching.CompareTitle 判定（版本标记不一致直接不匹配），
	// 不需要也不该再用一个说不清阈值的分数去压。
	//
	// Deprecated: 无效果。
	StripPenalty       float64              `json:"stripPenalty"`
	JaroWinklerMin     float64              `json:"jaroWinklerMin"`
	ReviewThreshold    float64              `json:"reviewThreshold"`
	PreferredCountries []string             `json:"preferredCountries"`
	MediumPriority     []string             `json:"mediumPriority"`
	MaxRetries         int                  `json:"maxRetries"`
	BackoffBaseSeconds float64              `json:"backoffBaseSeconds"`
	VariantStrategy    VariantStrategy      `json:"variantStrategy"`
	TracklistTiming    TracklistFetchTiming `json:"tracklistTiming"`
}

// ImportExportSettings 对应设置页的「导入导出」分组。
type ImportExportSettings struct {
	DefaultEncoding  string `json:"defaultEncoding"`
	DefaultDelimiter string `json:"defaultDelimiter"`
	MultiValueSep    string `json:"multiValueSep"`
	ExportFormat     string `json:"exportFormat"` // csv | json
	ExportUTF8BOM    bool   `json:"exportUtf8Bom"`
}

// CacheSettings 对应设置页的「缓存」分组。
type CacheSettings struct {
	CoverDir          string `json:"coverDir"`
	CoverMaxBytes     int64  `json:"coverMaxBytes"`
	CoverConcurrency  int    `json:"coverConcurrency"`
	ProviderCacheTTLh int    `json:"providerCacheTtlHours"`
	CoverTTLDays      int    `json:"coverTtlDays"`
}

// Settings 是全部用户可配置项。
type Settings struct {
	General          GeneralSettings      `json:"general"`
	MusicBrainz      MusicBrainzSettings  `json:"musicbrainz"`
	ITunes           ITunesSettings       `json:"itunes"`
	QQMusic          QQMusicSettings      `json:"qqmusic"`
	ProviderPriority []string             `json:"providerPriority"`
	Matching         MatchingSettings     `json:"matching"`
	ImportExport     ImportExportSettings `json:"importExport"`
	Cache            CacheSettings        `json:"cache"`
	OnboardingDone   bool                 `json:"onboardingDone"`
}

// Defaults 返回全部设置项的默认值。用户从未配置时系统即以这组值运行。
func Defaults() Settings {
	return Settings{
		General: GeneralSettings{Theme: "dark", Language: "zh-CN"},
		MusicBrainz: MusicBrainzSettings{
			Contact:    "",
			ServerURL:  "https://musicbrainz.org",
			RatePerSec: 1.0,
		},
		ITunes: ITunesSettings{
			DefaultCountry: "",
			RatePerMin:     20,
		},
		QQMusic:          QQMusicSettings{RatePerMin: provider.QQRatePerMinute().Default},
		ProviderPriority: []string{"musicbrainz", "itunes", "qq"},
		Matching: MatchingSettings{
			TitleWeight:        55,
			CreditExactWeight:  20,
			CreditFuzzyWeight:  12,
			AlbumExactWeight:   20,
			AlbumFuzzyWeight:   12,
			UpstreamScoreScale: 0.05,
			UpstreamScoreCap:   5,
			StripPenalty:       3,
			JaroWinklerMin:     0.90,
			ReviewThreshold:    75,
			PreferredCountries: []string{"JP", "CN", "US", "GB", "XW"},
			MediumPriority:     []string{"CD", "SACD", "Vinyl", "Cassette", "Digital Media"},
			MaxRetries:         5,
			BackoffBaseSeconds: 2,
			VariantStrategy:    VariantProgressive,
			TracklistTiming:    TracklistOnAlbumOpen,
		},
		ImportExport: ImportExportSettings{
			DefaultEncoding:  "auto",
			DefaultDelimiter: "auto",
			MultiValueSep:    "; ",
			ExportFormat:     "csv",
			ExportUTF8BOM:    true,
		},
		Cache: CacheSettings{
			CoverDir:          "",
			CoverMaxBytes:     2 << 30, // 2 GiB
			CoverConcurrency:  32,
			ProviderCacheTTLh: 24 * 30,
			CoverTTLDays:      30,
		},
	}
}

// NormalizeStartup returns the effective startup settings without rewriting disk.
func NormalizeStartup(loaded Settings) Settings {
	loaded.QQMusic.RatePerMin = provider.QQRatePerMinute().Normalize(loaded.QQMusic.RatePerMin)
	return loaded
}

// Validate rejects the entire save before directory, persistence or quota changes.
func Validate(next Settings) error {
	rule := provider.QQRatePerMinute()
	if !rule.Valid(next.QQMusic.RatePerMin) {
		return fmt.Errorf("QQ 音乐合计速率必须是 %g–%g 请求/分钟内的有限数值", rule.Min, rule.Max)
	}
	return nil
}

// Store 以 JSON 文件持久化设置，并对并发读写加锁。
type Store struct {
	mu   sync.RWMutex
	path string
	cur  Settings
}

// NewStore 从 appDir/settings.json 载入设置；文件不存在时使用默认值。
func NewStore(appDir string) (*Store, error) {
	s := &Store{path: filepath.Join(appDir, "settings.json"), cur: Defaults()}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	// 以默认值为底，未知/缺失字段保持默认——新增设置项无需迁移。
	loaded := Defaults()
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil, err
	}
	s.cur = NormalizeStartup(loaded)
	return s, nil
}

// Get 返回当前设置的快照。
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// Save 覆盖写入设置并落盘。
func (s *Store) Save(next Settings) error {
	if err := Validate(next); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path

	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	s.cur = next
	return nil
}

// Path 返回设置文件路径。
func (s *Store) Path() string { return s.path }
