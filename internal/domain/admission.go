package domain

// GroupEvidence 保存两条独立判组路径的来源，供预览、并发重验和归属解释使用。
type GroupEvidence struct {
	GroupID  int64     `json:"groupId"`
	Kind     string    `json:"kind"`
	SourceID string    `json:"sourceId"`
	Title    string    `json:"title"`
	Credit   string    `json:"credit"`
	Native   *Identity `json:"native,omitempty"`
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
}

type AdmissionOperation string

const (
	AdmissionCreate       AdmissionOperation = "admit"
	AdmissionSkipExisting AdmissionOperation = "skip_existing"
)

// AdmissionSeed 同时承载已归一化的上游事实和本地当前值；空身份层仅用于本地创建。
// 保存本地层时不将它伪装成上游快照。
type AdmissionSeed struct {
	Operation      AdmissionOperation `json:"operation"`
	InputScope     *InputScope        `json:"inputScope,omitempty"`
	ItemRevision   int64              `json:"itemRevision"`
	CurrentRelease *ReleaseSnapshot   `json:"currentRelease,omitempty"`
	EditJSON       string             `json:"editJson,omitempty"`
	MoveReleaseID  int64              `json:"moveReleaseId"`
	ProposalID     int64              `json:"proposalId"`
	Snapshot       Snapshot           `json:"snapshot"`
	ImportItemID   int64              `json:"importItemId"`
	MergeTargetID  int64              `json:"mergeTargetId"`
}
type AdmissionBaseline struct {
	InputScope              *InputScope     `json:"inputScope,omitempty"`
	NewNativeMapping        bool            `json:"newNativeMapping"`
	ItemRevision            int64           `json:"itemRevision"`
	Resolution              GroupResolution `json:"resolution"`
	ExistingTrackID         int64           `json:"existingTrackId"`
	ExistingTrackRevision   int64           `json:"existingTrackRevision"`
	ExistingReleaseRevision int64           `json:"existingReleaseRevision"`
	ExistingReleaseID       int64           `json:"existingReleaseId"`
	TargetGroupID           int64           `json:"targetGroupId"`
	RequiresConfirmation    bool            `json:"requiresConfirmation"`
	Conflict                bool            `json:"conflict"`
	ItemState               string          `json:"itemState"`
}
type AdmissionPlan struct {
	Seal      string            `json:"seal"`
	ID        string            `json:"id"`
	Hash      string            `json:"hash"`
	ExpiresAt string            `json:"expiresAt"`
	Seed      AdmissionSeed     `json:"seed"`
	Baseline  AdmissionBaseline `json:"baseline"`
}
type AdmissionApproval struct {
	PlanID     string `json:"planId"`
	Hash       string `json:"hash"`
	Credential string `json:"credential"`
}
type AdmissionInputResult struct {
	ItemID  int64  `json:"itemId"`
	BatchID int64  `json:"batchId"`
	TrackID int64  `json:"trackId"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
}

type AdmissionTarget struct {
	Track   Identity `json:"track"`
	Release Identity `json:"release"`
}

type AdmissionResult struct {
	Target    *AdmissionTarget       `json:"target,omitempty"`
	Inputs    []AdmissionInputResult `json:"inputs,omitempty"`
	TrackID   int64                  `json:"trackId"`
	ReleaseID int64                  `json:"releaseId"`
	GroupID   int64                  `json:"groupId"`
	Existing  bool                   `json:"existing"`
}
