package caa

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/norhua/sonexa/internal/provider"
)

// CoverArtArchiveBaseURL is independent of MusicBrainz API quota. Index and image
// requests are executed only through the injected cover pool.
const CoverArtArchiveBaseURL = "https://coverartarchive.org"

type caaIndex struct {
	Images  []caaImage `json:"images"`
	Release string     `json:"release"`
}
type caaImage struct {
	ID         json.Number       `json:"id"`
	Image      string            `json:"image"`
	Front      bool              `json:"front"`
	Back       bool              `json:"back"`
	Types      []string          `json:"types"`
	Thumbnails map[string]string `json:"thumbnails"`
}

// parseIndex owns the shared wire format and structural validation. Catalog
// identity/variant checks remain in its projection; thumbnail fallback needs
// only URLs and must not inherit the catalog's stricter persistence contract.
func parseIndex(body []byte) (*caaIndex, error) {
	var index caaIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("解析 CAA 索引失败: %w", err)
	}
	if index.Images == nil {
		return nil, fmt.Errorf("CAA 索引缺少 images 数组")
	}
	return &index, nil
}

func CAAFrontLadder(release, group string) []provider.CoverImage {
	var out []provider.CoverImage
	if release != "" {
		out = append(out, provider.CoverImage{Type: "Front", URL: CAAFrontURL(release, 1200)}, provider.CoverImage{Type: "Front", URL: CAAFrontURL(release, 500)})
	}
	if group != "" {
		out = append(out, provider.CoverImage{Type: "Front", URL: fmt.Sprintf("%s/release-group/%s/front-1200", CoverArtArchiveBaseURL, group)})
	}
	return out
}
func CAAFrontURL(release string, size int) string {
	return fmt.Sprintf("%s/release/%s/front-%d", CoverArtArchiveBaseURL, release, size)
}
func CAAThumbnailURL(release string) string { return CAAFrontURL(release, 250) }
func CAAIndexFallbackURLs(release, group string) []string {
	var urls []string
	if release != "" {
		urls = append(urls, CoverArtArchiveBaseURL+"/release/"+release)
	}
	if group != "" {
		urls = append(urls, CoverArtArchiveBaseURL+"/release-group/"+group)
	}
	return urls
}
func ParseCAAThumbnailIndex(rawURL string, body []byte) ([]string, bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != "coverartarchive.org" {
		return nil, false, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || (parts[0] != "release" && parts[0] != "release-group") {
		return nil, false, nil
	}
	index, err := parseIndex(body)
	if err != nil {
		return nil, true, err
	}
	var urls []string
	seen := map[string]bool{}
	for _, front := range []bool{true, false} {
		for _, img := range index.Images {
			if img.Front != front {
				continue
			}
			for _, raw := range []string{img.Thumbnails["250"], img.Thumbnails["500"], img.Thumbnails["1200"], img.Image} {
				if raw == "" {
					continue
				}
				u := secureCAAURL(raw)
				if !seen[u] {
					seen[u] = true
					urls = append(urls, u)
				}
			}
		}
	}
	return urls, true, nil
}
func secureCAAURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "http" && (u.Host == "coverartarchive.org" || u.Host == "archive.org" || strings.HasSuffix(u.Host, ".archive.org")) {
		u.Scheme = "https"
		return u.String()
	}
	return raw
}
