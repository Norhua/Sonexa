package store

import (
	"strconv"

	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

// QQCoverImages 统一封面搜索与自动取图的 QQ 图片身份和构造档位。
// 以专辑 MID 作为图片身份；档位是构造地址，实际可用性与尺寸须经下载解码确认。
func QQCoverImages(albumMID, title, artist string) []CoverImage {
	if !qqmusic.ValidMID(albumMID) {
		return nil
	}
	image := CoverImage{SourceImageID: albumMID, PrimaryType: "Front", Types: []string{"Front"}, Front: true, Title: title, Artist: artist}
	for _, size := range qqmusic.CoverSizes {
		image.Variants = append(image.Variants, CoverVariant{Spec: strconv.Itoa(size), Constructed: true, URL: qqmusic.CoverURL(albumMID, size)})
	}
	return []CoverImage{image}
}
