package lyrics

import (
	"fmt"
	"strings"
	"testing"
)

func TestMergeNeverDropsUntimedExtras(t *testing.T) {
	for _, original := range []string{"plain original", "[00:01]original", "[00:01]<00:01>original<00:02>"} {
		doc, err := channelDocument(TextChannel(original, nil), TextChannel("plain translation", nil), TextChannel("[00:01]roma\nuntimed tail", nil), "unavailable", nil)
		if err != nil || !strings.Contains(doc.Text, "plain translation") || !strings.Contains(doc.Text, "untimed tail") || !strings.Contains(doc.Text, "roma") {
			t.Fatalf("%+v %v", doc, err)
		}
	}
	got, err := mergeTimed("[00:01]original\n[00:02]two", "[00:02]second\n[00:01]first")
	if err != nil {
		t.Fatal(err)
	}
	if got != "[00:01.000]original\n[00:01.000]first\n[00:02.000]two\n[00:02.000]second" {
		t.Fatal(got)
	}
}
func TestTimestampExpansionIsBounded(t *testing.T) {
	var timestamps strings.Builder
	for i := 0; i < 1000; i++ {
		timestamps.WriteString(fmt.Sprintf("[%02d:%02d.000]", i/60, i%60))
	}
	if _, err := mergeTimed(timestamps.String()+strings.Repeat("a", 10000), "translation"); err == nil {
		t.Fatal("unbounded timestamp expansion")
	}
}
