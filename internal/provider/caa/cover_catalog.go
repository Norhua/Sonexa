package caa

import (
	"encoding/json"
	"fmt"
)

// CoverIndex 保留图片身份和目录档位，不把所有图片展开为同一降级链。
type CoverIndex struct {
	Release string
	Images  []IndexedCover
}

type IndexedCover struct {
	ID       string
	Types    []string
	Type     string
	Front    bool
	Back     bool
	Original string
	Variants []IndexedVariant
}

type IndexedVariant struct {
	Spec string
	URL  string
}

func ParseCoverCatalog(body []byte) (*CoverIndex, error) {
	var raw caaIndex
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析 CAA 目录失败: %w", err)
	}
	if raw.Images == nil {
		return nil, fmt.Errorf("CAA 目录缺少 images 数组")
	}
	out := &CoverIndex{Release: raw.Release, Images: []IndexedCover{}}
	seenIDs := map[string]bool{}
	for _, image := range raw.Images {
		id := image.ID.String()
		if id == "" || seenIDs[id] {
			return nil, fmt.Errorf("CAA 图片身份缺失或重复")
		}
		seenIDs[id] = true
		item := IndexedCover{ID: id, Types: image.Types, Front: image.Front, Back: image.Back, Type: "Other", Original: secureCAAURL(image.Image)}
		if len(image.Types) > 0 {
			item.Type = image.Types[0]
		}
		if item.Front {
			item.Type = "Front"
		}
		if item.Types == nil {
			item.Types = []string{}
		}
		seenURLs := map[string]bool{}
		for _, spec := range []string{"250", "500", "1200", "original"} {
			u := image.Thumbnails[spec]
			if spec == "original" {
				u = image.Image
			} else if u == "" && spec == "250" {
				u = image.Thumbnails["small"]
			} else if u == "" && spec == "500" {
				u = image.Thumbnails["large"]
			}
			u = secureCAAURL(u)
			if u != "" && !seenURLs[u] {
				seenURLs[u] = true
				item.Variants = append(item.Variants, IndexedVariant{Spec: spec, URL: u})
			}
		}
		if len(item.Variants) == 0 {
			return nil, fmt.Errorf("CAA 图片 %s 没有可用规格地址", id)
		}
		out.Images = append(out.Images, item)
	}
	return out, nil
}
