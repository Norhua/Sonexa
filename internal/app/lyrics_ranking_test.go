package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func TestAllLyricSourcesRankAndDeduplicate(t *testing.T) {
	fixtures := map[string]string{
		"qq":    `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[{"mid":"0039MnYb0qxYhV","title":"Wrong","singer":[{"name":"Alice"}]},{"mid":"001wMai814HLgJ","title":"Song","singer":[{"name":"Alice"}]},{"mid":"001wMai814HLgJ","title":"Song","singer":[{"name":"Alice"}]}]}}}}}`,
		"163":   `{"code":200,"result":{"songs":[{"id":1,"name":"Wrong","ar":[{"name":"Alice"}]},{"id":2,"name":"Song","ar":[{"name":"Alice"}]},{"id":2,"name":"Song","ar":[{"name":"Alice"}]}]}}`,
		"kugou": `{"status":1,"errcode":0,"data":{"info":[{"hash":"1","songname":"Wrong","singername":"Alice"},{"hash":"2","songname":"Song","singername":"Alice"},{"hash":"2","songname":"Song","singername":"Alice"}]}}`,
		"kuwo":  `{"TOTAL":"3","abslist":[{"MUSICRID":"1","NAME":"Wrong","ARTIST":"Alice"},{"MUSICRID":"2","NAME":"Song","ARTIST":"Alice"},{"MUSICRID":"2","NAME":"Song","ARTIST":"Alice"}]}`,
	}
	for source, body := range fixtures {
		t.Run(source, func(t *testing.T) {
			got, err := replayService(body).Search(context.Background(), source, lyrics.Query{Title: "Song", Artist: "Alice"})
			if err != nil || len(got) != 1 || !got[0].HighConfidence || got[0].Source != source {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
func TestLyricSearchUsesTargetDuration(t *testing.T) {
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Req struct{ Param struct{ Query string } }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Req.Param.Query != "Song Alice" {
			t.Fatalf("%+v %v", request, err)
		}
		body := `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[{"mid":"001wMai814HLgJ","title":"Song","singer":[{"name":"Alice"}],"interval":321}]}}}}}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := sourceService(client).Search(context.Background(), "qq", lyrics.Query{Title: "Song", Artist: "Alice", DurationMS: 321000})
	if err != nil || len(got) != 1 || got[0].MatchScore != 80 {
		t.Fatalf("%+v %v", got, err)
	}
}
