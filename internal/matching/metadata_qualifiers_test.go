package matching

import "testing"

func TestMetadataQualifierPayloads(t *testing.T) {
	for _, tt := range []struct {
		field MetadataField
		a, b  string
		want  EvidenceLevel
	}{
		{MetadataTitle, "Song (A Remix)", "Song (B Remix)", EvidenceConflict},
		{MetadataTitle, "Song - A Remix", "Song (A Remix)", EvidenceVerifiedAlias},
		{MetadataTitle, "Song (Live Tokyo 2020)", "Song (Live Osaka 2020)", EvidenceConflict},
		{MetadataTitle, "Song Live at Tokyo 2020", "Song (Live at Tokyo 2020)", EvidenceVerifiedAlias},
		{MetadataTitle, "you(Vocal)", "you (Off Vocal)", EvidenceConflict},
		{MetadataTitle, "Song (Instrumental)", "Song (off vocal)", EvidenceVerifiedAlias},
		{MetadataTitle, "Song (Re-recorded 2020)", "Song (Re-recorded 2021)", EvidenceConflict},
		{MetadataTitle, "Song (2020 Remastered)", "Song (2021 Remaster)", EvidenceConflict},
		{MetadataTitle, "Song 2020 Remastered", "Song (Remaster 2020)", EvidenceVerifiedAlias},
		{MetadataTitle, "Song (TV Size)", "Song (movie ver.)", EvidenceConflict},
		{MetadataAlbum, "Album (Deluxe Edition)", "Album", EvidenceConflict},
		{MetadataAlbum, "Album Deluxe Edition", "Album (Deluxe)", EvidenceVerifiedAlias},
		{MetadataAlbum, "Album (限定版)", "Album (Limited Edition)", EvidenceVerifiedAlias},
		{MetadataAlbum, "Album (Limited Edition A)", "Album (Limited Edition B)", EvidenceConflict},
		{MetadataAlbum, "Album (2019 Remastered)", "Album (2021 Remastered)", EvidenceConflict},
		{MetadataAlbum, "Album (JP)", "Album (US)", EvidenceConflict},
		{MetadataAlbum, "Album (CD)", "Album (digital)", EvidenceConflict},
	} {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			a, b := ParseMetadataText(tt.a, tt.field), ParseMetadataText(tt.b, tt.field)
			got := CompareMetadataText(a, b, MetadataTextContext{Field: tt.field, IndependentSupport: true})
			if got.Level != tt.want {
				t.Fatalf("got %+v want %s\nprofiles: %+v %+v", got, tt.want, a, b)
			}
			for _, q := range append(a.Qualifiers, b.Qualifiers...) {
				if q.Raw == "" {
					t.Fatal("lost qualifier text")
				}
			}
		})
	}
}
