package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/provider"
)

func TestCoverProjectionRetainsPicIdentityAndBound(t *testing.T) {
	client := provider.NewClient(nil, nil, "test")
	p := New(client)
	p.form = func(body []byte) (url.Values, error) {
		var request struct {
			S     string
			Limit int
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatal(err)
		}
		if request.S != "album artist" || request.Limit != 30 {
			t.Fatal(request)
		}
		return Form(body)
	}
	client.HTTP.Transport = protocolTransport(func(*http.Request) (*http.Response, error) {
		rows := []string{`{"name":"song","al":{"id":0,"name":"album","picUrl":"https://p1.music.126.net/native.jpg?old=1"}}`}
		for i := 1; i < 30; i++ {
			rows = append(rows, fmt.Sprintf(`{"name":"song","al":{"id":%d,"name":"album","picUrl":"https://p1.music.126.net/%d.jpg"}}`, i, i))
		}
		body := `{"code":200,"result":{"songs":[` + strings.Join(rows, ",") + `]}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	rows, err := p.SearchCovers(context.Background(), sources.Query{Album: "album", Artist: "artist"})
	if err != nil || len(rows) != 20 || rows[0].Key().SourceRef != "pic-native" {
		t.Fatal(rows, err)
	}
	image := rows[0].Input.Images[0]
	if image.SourceImageID != "pic-native" || image.OriginalURL != "https://p1.music.126.net/native.jpg" {
		t.Fatal(image)
	}
	for i, v := range image.Variants {
		if v.Width != nil || v.Height != nil || (i > 0 && !v.Constructed) {
			t.Fatal(v)
		}
	}
}
