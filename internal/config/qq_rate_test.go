package config

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestQQStartupNormalizationAndPersistence(t *testing.T) {
	for _, tc := range []struct {
		name, json string
		want       float64
	}{
		{"missing-file", "", 45}, {"missing-section", `{}`, 45}, {"missing-field", `{"qqmusic":{}}`, 45},
		{"old-value", `{"qqmusic":{"ratePerMin":30}}`, 30},
		{"minimum", `{"qqmusic":{"ratePerMin":1}}`, 1}, {"maximum", `{"qqmusic":{"ratePerMin":60}}`, 60},
		{"fraction", `{"qqmusic":{"ratePerMin":20.5}}`, 20.5},
		{"high", `{"qqmusic":{"ratePerMin":90}}`, 60}, {"low", `{"qqmusic":{"ratePerMin":0.5}}`, 1},
		{"zero", `{"qqmusic":{"ratePerMin":0}}`, 45}, {"negative", `{"qqmusic":{"ratePerMin":-1}}`, 45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")
			if tc.json != "" {
				if err := os.WriteFile(path, []byte(tc.json), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s, err := NewStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Get().QQMusic.RatePerMin; got != tc.want {
				t.Fatalf("rate=%g want=%g", got, tc.want)
			}
			if tc.json != "" {
				original, err := os.ReadFile(path)
				if err != nil || string(original) != tc.json {
					t.Fatal("startup rewrote settings", err)
				}
			}
			if err := s.Save(s.Get()); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewStore(dir)
			if err != nil || reopened.Get().QQMusic.RatePerMin != tc.want {
				t.Fatal("effective value not persisted", err)
			}
		})
	}
}

func TestQQSettingsRejectInvalidSavesWithoutSideEffects(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(s.Get()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []float64{0, -1, 0.5, 60.1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		next := s.Get()
		next.QQMusic.RatePerMin = value
		if err := s.Save(next); err == nil {
			t.Fatalf("accepted %g", value)
		}
		after, err := os.ReadFile(s.Path())
		if err != nil || !bytes.Equal(after, before) || s.Get().QQMusic.RatePerMin != 45 {
			t.Fatal("invalid save changed settings", err)
		}
		if got := NormalizeStartup(next).QQMusic.RatePerMin; !provider.QQRatePerMinute().Valid(got) {
			t.Fatalf("unsafe startup rate %g", got)
		}
	}
	for _, value := range []float64{1, 20.5, 30, 45, 60} {
		next := s.Get()
		next.QQMusic.RatePerMin = value
		if err := s.Save(next); err != nil {
			t.Fatal(err)
		}
		if s.Get().QQMusic.RatePerMin != value {
			t.Fatal("valid save changed value")
		}
	}
}

func TestQQBrokenJSONKeepsReadError(t *testing.T) {
	for _, body := range []string{`{"qqmusic":{"ratePerMin":NaN}}`, `{"qqmusic":{"ratePerMin":1e999}}`, `{"qqmusic":`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(dir); err == nil {
			t.Fatal("broken settings silently replaced")
		}
	}
}
