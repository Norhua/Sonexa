package netease

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

type protocolTransport func(*http.Request) (*http.Response, error)

func (f protocolTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWEAPISearchRequestAndEncryption(t *testing.T) {
	for _, limit := range []int{20, 30} {
		client := provider.NewClient(nil, nil, "test")
		p := New(client)
		// Capture plaintext at the encryption boundary, then use the real Form.
		p.form = func(plain []byte) (url.Values, error) {
			var q struct {
				S                   string
				Type, Limit, Offset int
			}
			if err := json.Unmarshal(plain, &q); err != nil {
				t.Fatal(err)
			}
			if q.S != "song artist" || q.Type != 1 || q.Limit != limit || q.Offset != 0 {
				t.Fatal(q)
			}
			return Form(plain)
		}
		calls := 0
		client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || r.URL.Path != searchPath || r.Header.Get("Referer") != baseURL+"/" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || len(r.Form.Get("encSecKey")) != 256 {
				t.Fatal("WEAPI 请求改变", r)
			}
			ciphertext, err := base64.StdEncoding.DecodeString(r.Form.Get("params"))
			if err != nil || len(ciphertext)%aes.BlockSize != 0 || len(ciphertext) == 0 {
				t.Fatal("加密表单无效")
			}
			if strings.Contains(r.Form.Encode(), "song") {
				t.Fatal("明文作为表单发送")
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"result":{"songCount":0}}`))}, nil
		})
		if rows, err := p.searchSongs(context.Background(), "song artist", limit); err != nil || len(rows) != 0 || calls != 1 {
			t.Fatalf("rows=%v calls=%d err=%v", rows, calls, err)
		}
	}
	// Verify the fixed AES layer roundtrips rather than just checking its shape.
	plain := []byte(`{"s":"歌曲","limit":20}`)
	encoded, err := weapiAES(plain, weapiPresetKey)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := base64.StdEncoding.DecodeString(encoded)
	block, _ := aes.NewCipher([]byte(weapiPresetKey))
	cipher.NewCBCDecrypter(block, []byte(weapiIV)).CryptBlocks(data, data)
	if string(data[:len(data)-int(data[len(data)-1])]) != string(plain) {
		t.Fatal("AES 协议改变")
	}
}

func TestNativeSongsPreserveCoverAndLyricsFields(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/golden/responses/covers/163_search.json")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseSongs(body)
	if err != nil || len(rows) == 0 || rows[0].Album.PicURL == "" || len(rows[0].Artists) == 0 {
		t.Fatal(rows, err)
	}
	rows, err = parseSongs([]byte(`{"code":200,"result":{"songs":[{"id":1234567890123456789,"name":"song","dt":1000,"ar":[{"name":"artist"}],"al":{"id":0,"name":"album","picUrl":"https://p1.music.126.net/pic.jpg"}}]}}`))
	if err != nil || len(rows) != 1 || rows[0].ID.String() != "1234567890123456789" || rows[0].Album.ID.String() != "0" || rows[0].DurationMS != 1000 {
		t.Fatal(rows, err)
	}
	for _, body := range []string{`{}`, `{"code":403}`, `{"code":200}`, `{"code":200,"result":{}}`, `{"code":200,"result":{"songCount":1}}`} {
		if _, err := parseSongs([]byte(body)); err == nil {
			t.Fatal("错误结构被接受", body)
		}
	}
	for _, body := range []string{`{"code":200,"result":{"songCount":0}}`, `{"code":200,"result":{"songs":[]}}`} {
		if rows, err := parseSongs([]byte(body)); err != nil || len(rows) != 0 {
			t.Fatal(rows, err)
		}
	}
}
