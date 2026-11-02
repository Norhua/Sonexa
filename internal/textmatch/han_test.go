package textmatch

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestHanDictionaryAndDirectEquivalence(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(hanDictionary))); got != "6b5a0a799bea2bb22c001f635eaa3fc2904310f0c08addbff275477a80ecf09a" {
		t.Fatal("unreviewed dictionary update", got)
	}
	variants := hanVariants()
	if variants['倫'] != '伦' || variants['陳'] != '陈' || variants['乾'] != 0 {
		t.Fatal("missing or ambiguous mapping")
	}
	for _, tt := range []struct {
		a, b string
		want bool
	}{
		{"周杰倫", "周杰伦", true}, {"早坂爱", "早坂愛", true}, {"乾", "干", false}, {"發", "髮", false},
		{"周杰倫", "周杰伦合唱团", false}, {"", "", false},
	} {
		if HanEquivalent(tt.a, tt.b) != tt.want || HanEquivalent(tt.b, tt.a) != tt.want {
			t.Fatal(tt)
		}
	}
}
