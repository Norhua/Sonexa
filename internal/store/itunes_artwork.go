package store

import (
	"strconv"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
)

// ITunesCoverImages 统一搜索与精确 Lookup 的图片身份和构造档位。
// 以固定 250 地址作为身份，避免同一图片因请求用途不同变成多张图片。
func ITunesCoverImages(artwork, title, artist string) []CoverImage {
	if artwork == "" {
		return nil
	}
	image := CoverImage{SourceImageID: itunes.ArtworkURL(artwork, 250), OriginalURL: artwork,
		PrimaryType: "Front", Types: []string{"Front"}, Front: true, Title: title, Artist: artist}
	for _, size := range itunes.ArtworkSizes {
		image.Variants = append(image.Variants, CoverVariant{Spec: strconv.Itoa(size), URL: itunes.ArtworkURL(artwork, size), Constructed: true})
	}
	return []CoverImage{image}
}

// RememberCandidateArtwork 只登记已知 iTunes 候选的目录，不下载、不创建歌集、
// 不改变采用关系。已存在的成功目录不重写，保留规格验证和失败记忆。
func (s *Store) RememberCandidateArtwork(c provider.Candidate) error {
	if c.Provider != provider.ITunes || deref(c.ReleaseRef) == "" || deref(c.ThumbURL) == "" {
		return nil
	}
	key := CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: *c.ReleaseRef}
	catalog, err := s.GetCoverCatalog(key)
	if err != nil || (catalog != nil && catalog.State == "found") {
		return err
	}
	_, err = s.SaveCoverCatalog(key, ITunesCoverImages(*c.ThumbURL, deref(c.AlbumTitle), deref(c.ArtistName)))
	return err
}
