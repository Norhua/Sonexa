package app

import "github.com/norhua/sonexa/internal/domain"

// AdmissionPlanView 仅投影确认页所需内容，完整计划及签名只留在服务端。
// 使用具体 DTO，避免 Wails v2 对泛型数组生成不可用构造器。
type PlanTextField struct {
	State domain.FieldState `json:"state"`
	Value string            `json:"value"`
}
type PlanCredit struct {
	Text string `json:"text"`
}
type PlanCreditField struct {
	State domain.FieldState `json:"state"`
	Value PlanCredit        `json:"value"`
}
type PlanReleaseView struct {
	Identity domain.Identity `json:"identity"`
	Title    PlanTextField   `json:"title"`
	Credit   PlanCreditField `json:"credit"`
}
type PlanSnapshotView struct {
	Release *PlanReleaseView `json:"release"`
}
type PlanSeedView struct {
	Snapshot      PlanSnapshotView `json:"snapshot"`
	MoveReleaseID int64            `json:"moveReleaseId"`
	MergeTargetID int64            `json:"mergeTargetId"`
	JoinGroupID   int64            `json:"joinGroupId"`
	KeepSeparate  bool             `json:"keepSeparate"`
	// IsEdit 为真表示这是编辑器保存计划：不能在弹窗里改用其他发行。
	IsEdit bool `json:"isEdit"`
}
type AdmissionPlanView struct {
	ID        string                   `json:"id"`
	Hash      string                   `json:"hash"`
	ExpiresAt string                   `json:"expiresAt"`
	Baseline  domain.AdmissionBaseline `json:"baseline"`
	Seed      PlanSeedView             `json:"seed"`
}

func admissionView(plan *domain.AdmissionPlan, err error) (*AdmissionPlanView, error) {
	if err != nil || plan == nil {
		return nil, err
	}
	view := &AdmissionPlanView{ID: plan.ID, Hash: plan.Hash, ExpiresAt: plan.ExpiresAt, Baseline: plan.Baseline, Seed: PlanSeedView{MoveReleaseID: plan.Seed.MoveReleaseID, MergeTargetID: plan.Seed.MergeTargetID,
		JoinGroupID: plan.Seed.JoinGroupID, KeepSeparate: plan.Seed.KeepSeparate, IsEdit: plan.Seed.EditJSON != ""}}
	if r := plan.Seed.Snapshot.Release; r != nil {
		view.Seed.Snapshot.Release = &PlanReleaseView{Identity: r.Identity, Title: PlanTextField{r.Title.State, r.Title.Value}, Credit: PlanCreditField{r.Credit.State, PlanCredit{r.Credit.Value.Text}}}
	}
	return view, nil
}

type GroupProposalView struct {
	ID               int64                  `json:"id"`
	ReleaseID        int64                  `json:"releaseId"`
	BaselineRevision int64                  `json:"baselineRevision"`
	Status           string                 `json:"status"`
	Resolution       domain.GroupResolution `json:"resolution"`
	Snapshot         PlanSnapshotView       `json:"snapshot"`
}
