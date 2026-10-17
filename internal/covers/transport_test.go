package covers

import (
	"github.com/norhua/sonexa/internal/provider/caa"
	"testing"
)

func TestImageTransportRejectsResolvedPrivateAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1:443", "10.1.1.1:443", "172.16.0.1:443", "192.168.1.1:443", "169.254.169.254:443", "100.64.1.1:443", "[::1]:443", "[::ffff:127.0.0.1]:443", "[fc00::1]:443", "[fe80::1]:443", "[ff02::1]:443", "0.0.0.0:443", "224.0.0.1:443"} {
		if err := publicImageAddress(address); err == nil {
			t.Error("接受了非公开地址", address)
		}
	}
	for _, address := range []string{"8.8.8.8:443", "[2606:4700:4700::1111]:443"} {
		if err := publicImageAddress(address); err != nil {
			t.Error(address, err)
		}
	}
}

func TestCAAImageLadder(t *testing.T) {
	m := "76df3287-6cda-33eb-8e9a-044b5e15ffdd"
	cases := map[string]string{
		"https://coverartarchive.org/release/" + m + "/829521842-250.jpg":  "https://archive.org/download/mbid-" + m + "/mbid-" + m + "-829521842_thumb250.jpg",
		"https://coverartarchive.org/release/" + m + "/829521842-1200.jpg": "https://archive.org/download/mbid-" + m + "/mbid-" + m + "-829521842_thumb1200.jpg",
		"https://coverartarchive.org/release/" + m + "/829521842.png":      "https://archive.org/download/mbid-" + m + "/mbid-" + m + "-829521842.png",
	}
	for in, want := range cases {
		got := caa.ImageURLs(in)
		if len(got) != 2 || got[0] != want || got[1] != in {
			t.Fatalf("%s => %v", in, got)
		}
	}
	for _, other := range []string{"https://is1.mzstatic.com/a/100x100bb.jpg", "https://coverartarchive.org/release/" + m + "/front-250"} {
		if got := caa.ImageURLs(other); len(got) != 1 || got[0] != other {
			t.Fatal(other, got)
		}
	}
}
