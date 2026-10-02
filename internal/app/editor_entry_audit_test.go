package app

import (
	"github.com/norhua/sonexa/internal/domain"
	"reflect"
	"testing"
)

func TestEditorLegacyWriteBindingsAreRemoved(t *testing.T) {
	for _, name := range []string{"ApplyEdits", "RematchTrack", "SetProvenanceManual", "ClearProvenance", "UpdateTrackBasic", "PlanAlbumRename", "ApplyAlbumRename"} {
		if _, ok := reflect.TypeOf(&App{}).MethodByName(name); ok {
			t.Fatalf("obsolete binding %s remains exposed", name)
		}
	}
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	if _, err := a.SaveEdit("invented", "hash", domain.AdmissionApproval{}); err == nil {
		t.Fatal("unplanned save accepted")
	}
}
