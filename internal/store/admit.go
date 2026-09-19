package store

import "github.com/norhua/sonexa/internal/provider"

// AdmitCandidate 把一条上游候选写成曲库里的一行，并登记它所属的歌集。
//
// 这是候选入库的**唯一**落点。四条路径全部经由它：导入的自动入库、复核队列的
// 「接受」、搜索页的「加入曲库」、专辑视图灰色行的「加入曲库」。
//
// **只写候选自己带的字段。** 表格里的原始输入不进曲目：那一行是「用户当时怎么写」，
// 而曲目记录的是「上游怎么说」。上游没给的字段就留空，让它作为缺失如实呈现。
//
// 发行级字段（类型、载体、国家、厂牌、目录号、碟数、音轨数）随候选一起
// 写到**歌集**上而不是曲目上——它们是这张歌集的事实，同一张歌集的十二首
// 曲目没有理由各存一份。
//
// 撞唯一约束（同一候选被另一条路径抢先入库）时返回 *ErrAlreadyExists，
// 调用方据此静默转为已有曲目，不向用户报错。
func (s *Store) AdmitCandidate(c provider.Candidate) (int64, error) {
	if c.Title == nil || *c.Title == "" {
		return 0, ErrTitleRequired
	}
	source := string(c.Provider)

	return s.CreateTrackWithProvenance(NewTrackInput{
		Title:       *c.Title,
		Credit:      c.ArtistName,
		Album:       c.AlbumTitle,
		AlbumCredit: c.ArtistName,
		MatchSource: &source,
		MatchRef:    &c.MatchRef,
		Metadata: &MetadataInput{
			TrackNumber:  c.TrackNumber,
			DiscNumber:   c.DiscNumber,
			DurationMS:   c.DurationMS,
			RecordingRef: c.RecordingRef,
			ISRC:         c.ISRC,
		},
		Release: &ReleaseInput{
			Provider:         &source,
			ProviderRef:      c.ReleaseRef,
			ReleaseGroupRef:  c.ReleaseGroupRef,
			PrimaryType:      c.PrimaryType,
			ReleaseStatus:    c.ReleaseStatus,
			MediumFormat:     c.MediumFormat,
			Country:          c.Country,
			FirstReleaseDate: c.FirstReleaseDate,
			TrackTotal:       c.TrackTotal,
			Label:            c.Label,
			CatalogNumber:    c.CatalogNumber,
		},
	})
}
