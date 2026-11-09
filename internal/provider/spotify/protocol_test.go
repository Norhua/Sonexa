package spotify

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/norhua/sonexa/internal/playlists"
)

func TestSharedGoPythonProtocolFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/protocol.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Protocol  int    `json:"protocol"`
		RequestID string `json:"requestId"`
		Cases     []struct {
			Name  string          `json:"name"`
			Entry playlists.Entry `json:"entry"`
		} `json:"cases"`
		Empty, Failure json.RawMessage
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Protocol != protocolVersion || fixture.RequestID != "shared-fixture" {
		t.Fatal("protocol drift")
	}
	entry := fixture.Cases[0].Entry
	if *entry.DurationMS != 123456 || len(entry.Artists) != 2 || entry.Artists[0] != "署名一" || entry.Artists[1] != "署名二" {
		t.Fatal(entry)
	}
	missing := fixture.Cases[1].Entry
	if missing.Title != nil || missing.Album != nil || missing.Artists != nil || missing.DurationMS != nil {
		t.Fatal(missing)
	}
	for _, message := range []json.RawMessage{fixture.Empty, fixture.Failure} {
		var output bytes.Buffer
		if err = writeFrame(&output, message); err != nil {
			t.Fatal(err)
		}
		body, err := readFrame(&output)
		if err != nil {
			t.Fatal(err)
		}
		var original, roundTrip any
		_ = json.Unmarshal(message, &original)
		_ = json.Unmarshal(body, &roundTrip)
		a, _ := json.Marshal(original)
		b, _ := json.Marshal(roundTrip)
		if !bytes.Equal(a, b) {
			t.Fatal("frame changed")
		}
	}
}
