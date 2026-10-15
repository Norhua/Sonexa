package app

import "github.com/norhua/sonexa/internal/store"

// displayCVParticipants 只改变详情读模型：派生演唱用完整 CV 成员的名字和链接。
// 写入投影中，独立声优紧跟完整 CV 成员，且不占可点击区间；按实际 artist_id
// 关联而非姓名匹配，避免同名人物或其他合作署名被误列为演唱。
func displayCVParticipants(groups []ParticipantGroup, entries []store.CreditEntry, text string) []ParticipantGroup {
	validSpans := map[int]CreditSpan{}
	for _, span := range creditSpans(entries, text) {
		validSpans[span.Start] = span
	}
	byActor := map[int64][]CreditSpan{}
	for i := 1; i < len(entries); i++ {
		actor, whole := entries[i], entries[i-1]
		if actor.SpanStart != nil || actor.SpanEnd != nil || actor.Position != whole.Position+1 {
			continue
		}
		if whole.SpanStart == nil || whole.SpanEnd == nil {
			continue
		}
		span, valid := validSpans[*whole.SpanStart]
		if !valid || span.ArtistID != whole.ArtistID || span.End != *whole.SpanEnd {
			continue
		}
		byActor[actor.ArtistID] = append(byActor[actor.ArtistID], span)
	}
	for i, group := range groups {
		if group.Role != string(store.RoleVocal) {
			continue
		}
		var artists []Participant
		seen := map[int64]bool{}
		for _, artist := range group.Artists {
			spans := byActor[artist.ArtistID]
			if artist.Source == nil || *artist.Source != store.CVCreditSource || len(spans) == 0 {
				artists = append(artists, artist)
				continue
			}
			for _, span := range spans {
				if seen[span.ArtistID] {
					continue
				}
				seen[span.ArtistID] = true
				shown := artist
				shown.ArtistID, shown.Name = span.ArtistID, span.Text
				shown.Identity = nil // 完整 CV 身份不能借用声优的上游标识。
				artists = append(artists, shown)
			}
		}
		groups[i].Artists = artists
	}
	return groups
}
