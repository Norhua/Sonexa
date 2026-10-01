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

// AdmissionSeed 同时承载已归一化的上游事实和本地当前值；空身份层仅用于本地创建。
// 保存本地层时不将它伪装成上游快照。
type AdmissionSeed struct {
	MoveReleaseID int64    `json:"moveReleaseId"`
	ProposalID    int64    `json:"proposalId"`
	Snapshot      Snapshot `json:"snapshot"`
	ImportItemID  int64    `json:"importItemId"`
	ItemStatus    string   `json:"itemStatus"`
	MergeTargetID int64    `json:"mergeTargetId"`
}
type AdmissionBaseline struct {
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
type AdmissionApproval struct {
	PlanID     string `json:"planId"`
	Hash       string `json:"hash"`
	Credential string `json:"credential"`
}
type AdmissionResult struct {
	TrackID   int64 `json:"trackId"`
	ReleaseID int64 `json:"releaseId"`
	GroupID   int64 `json:"groupId"`
	Existing  bool  `json:"existing"`
}
