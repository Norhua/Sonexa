package store

import "github.com/norhua/sonexa/internal/provider"

// AdmitCandidate 把一条上游候选写成曲库里的一行，并登记它所属的发行。
//
// 这是候选入库的**唯一**落点。四条路径全部经由它：导入的自动入库、复核队列的
// 「接受」、搜索页的「加入曲库」、专辑视图灰色行的「加入曲库」。
// 各写各的时候三处漂了三个方向——导入多写了发行状态与碟号、搜索页少写了它们，
// 而复核的「接受」在候选缺署名时拿导入文件里的值去补。
//
// **只写候选自己带的字段。** 表格里的原始输入不进曲目：那一行是「用户当时怎么写」，
// 而曲目记录的是「上游怎么说」。上游没给的字段就留空，让它作为缺失如实呈现——
// 拿导入值去补会让一首曲目的署名一半来自上游、一半来自表格，且看不出哪一半是哪一半。
//
// 撞唯一约束（同一候选被另一条路径抢先入库）时返回 *ErrAlreadyExists，
// 调用方据此静默转为已有曲目，不向用户报错。
func (s *Store) AdmitCandidate(c provider.Candidate) (int64, error) {
	if c.Title == nil || *c.Title == "" {
		return 0, ErrTitleRequired
	}
	source := string(c.Provider)

	trackID, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: *c.Title, Artist: c.ArtistName, Album: c.AlbumTitle,
		MatchSource: &source, MatchRef: &c.MatchRef,
		Metadata: &MetadataInput{
			Title:            c.Title,
			Album:            c.AlbumTitle,
			TrackCredit:      c.ArtistName,
			PrimaryType:      c.PrimaryType,
			ReleaseStatus:    c.ReleaseStatus,
			MediumFormat:     c.MediumFormat,
			Country:          c.Country,
			Year:             c.Year,
			FirstReleaseDate: c.FirstReleaseDate,
			TrackNumber:      c.TrackNumber,
			TrackTotal:       c.TrackTotal,
			DiscNumber:       c.DiscNumber,
			DurationMS:       c.DurationMS,
			Label:            c.Label,
			CatalogNumber:    c.CatalogNumber,
			RecordingMBID:    c.RecordingRef,
			ReleaseMBID:      c.ReleaseRef,
			ReleaseGroupMBID: c.ReleaseGroupRef,
			ISRC:             c.ISRC,
		},
	})
	if err != nil {
		return 0, err
	}

	// 顺带登记发行，清单状态为 none——搜索响应不含完整清单，
	// 清单在首次打开该专辑时懒加载。
	if c.ReleaseRef != nil && *c.ReleaseRef != "" && c.AlbumTitle != nil {
		_, _ = s.UpsertRelease(&Release{
			Provider:         source,
			ProviderRef:      *c.ReleaseRef,
			ReleaseGroupRef:  c.ReleaseGroupRef,
			Title:            *c.AlbumTitle,
			AlbumCredit:      c.ArtistName,
			PrimaryType:      c.PrimaryType,
			ReleaseStatus:    c.ReleaseStatus,
			MediumFormat:     c.MediumFormat,
			Country:          c.Country,
			FirstReleaseDate: c.FirstReleaseDate,
			TrackTotal:       c.TrackTotal,
			TracklistState:   TracklistNone,
		})
	}
	return trackID, nil
}
