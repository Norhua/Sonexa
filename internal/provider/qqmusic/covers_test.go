package qqmusic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/provider"
)

func TestCoverSearchUsesNativeSongsAndBusinessQuery(t *testing.T) {
	for _, q := range []sources.Query{{Title: "song", Artist: "artist", Album: "album"}, {Artist: "artist", Album: "album"}} {
		client := provider.NewClient(nil, nil, "test")
		client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
			var request struct {
				Req struct {
					Param struct {
						Query string
						Limit int `json:"num_per_page"`
					}
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Req.Param.Query != sources.SearchTerm(q) || request.Req.Param.Limit != 30 {
				t.Fatal(request)
			}
			rows := []string{}
			for i := 0; i < 30; i++ {
				// Deliberately omit song MID and album numeric ID: neither is cover identity.
				rows = append(rows, fmt.Sprintf(`{"name":"song","singer":[{"name":"artist"}],"album":{"mid":"album%09d","name":"album"}}`, i))
			}
			body := `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[` + strings.Join(rows, ",") + `]}}}}}`
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		rows, err := New(client, Config{}, nil).SearchCovers(context.Background(), q)
		if err != nil || len(rows) != 20 {
			t.Fatal(len(rows), err)
		}
		for _, r := range rows {
			for _, v := range r.Input.Images[0].Variants {
				if !v.Constructed || v.Width != nil || v.Height != nil {
					t.Fatal(v)
				}
			}
		}
	}
}
