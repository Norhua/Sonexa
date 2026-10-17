package sources_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// 显式启用，普通测试保持离线。只获取公开封面，不含账号/代理/音频。
func TestLiveCoverSources(t *testing.T) {
	if os.Getenv("SONEXA_LIVE_COVERS") != "1" {
		t.Skip("set SONEXA_LIVE_COVERS=1 for public source validation")
	}
	for _, id := range []string{"qq", "163"} {
		t.Run(id, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			adapter := sources.NewWebSource(id, "Sonexa/test (public cover compatibility check)", provider.NewLimiters(), nil)
			results, err := adapter.Search(ctx, provider.Query{Title: "世界中の雨", Artist: "チームドラゴン from AKB48", Album: "心の羽根", Limit: 3})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) == 0 {
				t.Fatal("已知公开查询没有返回封面")
			}
			// QQ 1200 并非每张专辑都有，取 800；网易云取原图。
			var variant store.CoverVariant
			for _, v := range results[0].Input.Images[0].Variants {
				if v.Spec == "800" || v.Spec == "original" {
					variant = v
				}
			}
			request, err := http.NewRequestWithContext(ctx, "GET", variant.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := adapter.HTTP.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatal(response.Status)
			}
			data, err := covers.ReadImageBytes(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			info, err := covers.InspectImage(data)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("source=%s album=%s requested=%s actual=%dx%d format=%s bytes=%d", id, results[0].Key().SourceRef, variant.Spec, info.Width, info.Height, info.Format, info.Bytes)
		})
	}
}
