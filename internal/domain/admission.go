package domain

// GroupEvidence 保存两条独立判组路径的来源，供预览、并发重验和归属解释使用。
type GroupEvidence struct {
	GroupID  int64     `json:"groupId"`
	Kind     string    `json:"kind"`
	SourceID string    `json:"sourceId"`
	Title    string    `json:"title"`
	Credit   string    `json:"credit"`
	Native   *Identity `json:"native,omitempty"`
	// Reason 只在被否决的证据上出现："native_mismatch"（同来源原生组不同）
	// 或 "separated"（用户此前判定这两组不是同一组）。
	Reason string `json:"reason,omitempty"`
}
type ReleaseMember struct {
	ID       int64     `json:"id"`
	GroupID  int64     `json:"groupId"`
	Identity *Identity `json:"identity,omitempty"`
	Title    *string   `json:"title"`
	Credit   *string   `json:"credit"`
	Revision int64     `json:"revision"`
	TrackIDs []int64   `json:"trackIds"`
}
type GroupMatch struct {
	ID       int64           `json:"id"`
	Title    *string         `json:"title"`
	Credit   *string         `json:"credit"`
	Revision int64           `json:"revision"`
	Members  []ReleaseMember `json:"members"`
}
type GroupResolution struct {
	Evidence     []GroupEvidence `json:"evidence"`
	Groups       []GroupMatch    `json:"groups"`
	Insufficient bool            `json:"insufficient"`
	// Rejected 是名称命中但被否决的证据，只用于展示与日志，不进入 Groups。
	Rejected []GroupEvidence `json:"rejected,omitempty"`
	// Similar 是没有命中、但去掉版本后缀与署名附注后同名的组，仅作提示。
	Similar []GroupEvidence `json:"similar,omitempty"`
}

// SongFacts 是判断「是不是同一首歌」所需的事实，字段与 matching.SongFacts 同构；
// 领域层不依赖 matching，转换由消费方完成。
type SongFacts struct {
	Provider     ProviderID `json:"provider,omitempty"`
	RecordingRef string     `json:"recordingRef,omitempty"`
	Title        string     `json:"title,omitempty"`
	Credit       string     `json:"credit,omitempty"`
	DurationMS   *int       `json:"durationMs,omitempty"`
	// TrackID 是这首歌已有的本地曲目（编辑器迁移时即被编辑的曲目）；MovingTrackIDs 是
	// 同一次保存里一起迁走的其他曲目。它们即将离开原发行，不算原发行的曲目引用，也不
	// 作为本地发行的清单条目参与比较——否则这首歌永远「能由原发行提供」，而一个迁走后
	// 就变空的原发行还会把结论拖成「不可判定」。
	TrackID        int64   `json:"trackId,omitempty"`
	MovingTrackIDs []int64 `json:"movingTrackIds,omitempty"`
}

// CoverageConclusion 是「组内已有发行是否包含这首歌」的结论。
type CoverageConclusion string

const (
	// CoverageProvided：组内已有发行能提供这首歌（录音级证据）。
	CoverageProvided CoverageConclusion = "provided"
	// CoveragePossible：文本级疑似能提供。
	CoveragePossible CoverageConclusion = "possible"
	// CoverageUnknown：有发行无法判定（清单不完整）。
	CoverageUnknown CoverageConclusion = "unknown"
	// CoverageAbsentProven：组内没有发行含这首歌（录音级已证明）。
	CoverageAbsentProven CoverageConclusion = "absent_proven"
	// CoverageAbsentText：组内没有发行含这首歌，但只有文本级证据。
	CoverageAbsentText CoverageConclusion = "absent_text"
	// CoverageNotApplicable：这次准入不涉及单首歌的归属（整发行换组、不换发行的编辑）。
	CoverageNotApplicable CoverageConclusion = "not_applicable"
)

// SongProvider 是组内已有发行中与这首歌对应的那一条清单条目。
type SongProvider struct {
	ReleaseID    int64     `json:"releaseId"`
	Identity     *Identity `json:"identity,omitempty"`
	Title        *string   `json:"title,omitempty"`
	Credit       *string   `json:"credit,omitempty"`
	DiscNumber   int       `json:"discNumber,omitempty"`
	TrackNumber  int       `json:"trackNumber,omitempty"`
	TrackTitle   *string   `json:"trackTitle,omitempty"`
	DurationMS   *int      `json:"durationMs,omitempty"`
	TrackRef     string    `json:"trackRef,omitempty"`
	RecordingRef string    `json:"recordingRef,omitempty"`
	// Evidence 是 "recording"（录音相同）或 "text"（疑似：曲名与时长相同）。
	Evidence  string `json:"evidence"`
	TrackID   int64  `json:"trackId,omitempty"`
	InLibrary bool   `json:"inLibrary,omitempty"`
	// TrackRefs / TrackTotal 是所在发行的曲目引用数与清单总曲数，即主发行排序的依据。
	TrackRefs  int `json:"trackRefs"`
	TrackTotal int `json:"trackTotal"`
}

// ReleaseCoverage 是一个无法判定的组内发行及其原因。
type ReleaseCoverage struct {
	ReleaseID int64     `json:"releaseId"`
	Identity  *Identity `json:"identity,omitempty"`
	Title     *string   `json:"title,omitempty"`
	Credit    *string   `json:"credit,omitempty"`
	Reason    string    `json:"reason"`
}

// AdmissionSeed 同时承载已归一化的上游事实和本地当前值；空身份层仅用于本地创建。
// 保存本地层时不将它伪装成上游快照。
type AdmissionSeed struct {
	Destinations    []ImportTarget   `json:"destinations"`
	CurrentRelease  *ReleaseSnapshot `json:"currentRelease,omitempty"`
	EditJSON        string           `json:"editJson,omitempty"`
	MoveReleaseID   int64            `json:"moveReleaseId"`
	ProposalID      int64            `json:"proposalId"`
	Snapshot        Snapshot         `json:"snapshot"`
	ImportItemID    int64            `json:"importItemId"`
	AutomaticImport bool             `json:"automaticImport"`
	ItemStatus      string           `json:"itemStatus"`
	MergeTargetID   int64            `json:"mergeTargetId"`
	// JoinGroupID 加入命中的某一组但不合并其余命中组；KeepSeparate 不加入任何
	// 命中组、独立成组。两者都会留下「不是同一组」的否定决定。三种判组动作互斥。
	JoinGroupID  int64 `json:"joinGroupId,omitempty"`
	KeepSeparate bool  `json:"keepSeparate,omitempty"`
	// CoverageSong 是编辑器迁移（曲目换发行或新建发行）时被编辑曲目的歌曲事实。
	// 编辑计划的曲目快照是空的，没有它覆盖结论恒为「不适用」，闸门形同虚设。
	// 只用于计算覆盖结论，不参与写入。
	CoverageSong *SongFacts `json:"coverageSong,omitempty"`
	// ReuseTrackID 复用一条没有上游身份的本地曲目（「使用发行 A」而提供者是本地曲目）。
	ReuseTrackID int64 `json:"reuseTrackId,omitempty"`
}
type ImportTarget struct {
	InputID          int64  `json:"inputId"`
	Kind             string `json:"kind"`
	EntryID          int64  `json:"entryId"`
	PlaylistID       int64  `json:"playlistId"`
	Revision         int64  `json:"revision"`
	EntryRevision    int64  `json:"entryRevision"`
	PlaylistRevision int64  `json:"playlistRevision"`
}
type AdmissionBaseline struct {
	Destinations []ImportTarget `json:"destinations"`
	// Coverage 是「组内已有发行是否包含这首歌」的结论；Providers 按主发行在前排序，
	// Undecidable 是清单不完整、给不出结论的发行。
	Coverage    CoverageConclusion `json:"coverage"`
	Providers   []SongProvider     `json:"providers,omitempty"`
	Undecidable []ReleaseCoverage  `json:"undecidable,omitempty"`
	// NeedsTypedConfirm：这次提交会让一首歌进入并非主发行的发行，或无法排除组内已有
	// 发行能提供它，必须由用户输入确认词；自动路径永远不会带这种凭证。
	NeedsTypedConfirm    bool            `json:"needsTypedConfirm"`
	NewNativeMapping     bool            `json:"newNativeMapping"`
	ItemRevision         string          `json:"itemRevision"`
	Resolution           GroupResolution `json:"resolution"`
	ExistingTrackID      int64           `json:"existingTrackId"`
	ExistingReleaseID    int64           `json:"existingReleaseId"`
	TargetGroupID        int64           `json:"targetGroupId"`
	RequiresConfirmation bool            `json:"requiresConfirmation"`
	Conflict             bool            `json:"conflict"`
	ItemState            string          `json:"itemState"`
}
type AdmissionPlan struct {
	Seal      string            `json:"seal"`
	ID        string            `json:"id"`
	Hash      string            `json:"hash"`
	ExpiresAt string            `json:"expiresAt"`
	Seed      AdmissionSeed     `json:"seed"`
	Baseline  AdmissionBaseline `json:"baseline"`
}

// 批准凭证的两种用途，按不同前缀签名，互不通用。
const (
	ApprovalMembership = ""
	ApprovalTyped      = "typed"
)

type AdmissionApproval struct {
	PlanID     string `json:"planId"`
	Hash       string `json:"hash"`
	Credential string `json:"credential"`
	// Kind 为空是普通成员确认；"typed" 是输入确认词后签发的凭证。
	Kind string `json:"kind"`
}
type AdmissionResult struct {
	PlaylistIDs []int64 `json:"playlistIds"`
	// RequeuedBatches 是本次提交顺带重新排队的复核条目所属批次，由应用层启动驱动器。
	RequeuedBatches []int64 `json:"requeuedBatches,omitempty"`
	TrackID         int64   `json:"trackId"`
	ReleaseID       int64   `json:"releaseId"`
	GroupID         int64   `json:"groupId"`
	Existing        bool    `json:"existing"`
}
