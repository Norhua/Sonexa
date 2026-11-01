package matching

import (
	"strings"

	"github.com/norhua/sonexa/internal/domain"
)

var reasonMessages = map[string]string{
	"normalized_text":                           "名称规范化后相同",
	"shared_complete_name":                      "可靠异写对应同一完整名称",
	"verified_alias":                            "通过可靠异写对应名称",
	"compatible_truncation":                     "截断名称与完整名称相容，仍需其他证据",
	"distinctive_album_truncation":              "有区分力的专辑截断已由完整目标资料确认",
	"different_truncated_prefix":                "截断前缀与目标名称不符",
	"unresolved_truncation":                     "截断内容不足以确定目标",
	"different_qualifier":                       "录音版本或发行限定不同",
	"unknown_annotation":                        "附加注释含义不明，不能自动视为相同",
	"uncorroborated_alias":                      "备选异写缺少独立证据支持",
	"unverified_translation":                    "译名之间尚无可靠对应证据",
	"similar_text":                              "名称相似，但不足以证明身份",
	"different_text":                            "名称不符",
	"missing_text":                              "名称信息缺失",
	"same_credit_members":                       "角色及合作署名一致",
	"source_credit_relation":                    "来源署名关系解释了艺术家差异",
	"different_credit_members":                  "署名成员或合作阵容不同",
	"different_credit_role":                     "角色或声优署名不同",
	"stale_rule_version":                        "输入证据规则已过期，需要重新判断",
	"stale_fact_version":                        "候选事实规则已过期，需要重新判断",
	"reliable_title_required":                   "缺少可靠的曲名对应证据",
	"target_album_unresolved":                   "目标专辑资料不足，尚不能确定",
	"credit_unresolved":                         "署名差异尚未解释",
	"supporting_name_required":                  "缺少署名或目标专辑的可靠支持",
	"numeric_evidence_unresolved":               "位置或时间信息无法可靠比较",
	"missing_number":                            "未提供该数值，不作为正证据",
	"unknown_number":                            "数值缺失或无法解析",
	"different_numeric_semantics":               "数值的单位或含义不同",
	"same_number":                               "数值要求相同",
	"unspecified_year_semantics":                "年份含义不明确，仅作为旁证",
	"duration_within_3s":                        "时长差在 3 秒内，支持匹配",
	"duration_within_10s":                       "时长差在 3 至 10 秒内，仅弱相容",
	"duration_conflict":                         "时长相差超过 10 秒，不能自动处理",
	"position_conflict":                         "碟号或轨号与目标位置冲突",
	"position_unresolved":                       "具体曲目位置尚未确定",
	"position_ambiguous":                        "同一发行存在多个可能位置，需要明确选择",
	"local_recall_truncated":                    "本地候选召回已截断，不能据此判断唯一",
	"local_target_missing_from_source":          "本地目标未在当前来源清单中确认",
	"decision_facts_unavailable":                "必要的来源决策资料尚不可用",
	"local_target_album_required":               "纯本地判断需要明确目标专辑",
	"unresolved_competitor":                     "仍有可能影响选择的未知候选",
	"current_cannot_override_source_constraint": "用户显示值不能覆盖来源的版本或位置冲突",
	"candidate_fact_conflict":                   "同一候选的来源事实互相冲突",
	"candidate_identity_unavailable":            "候选精确身份尚不可用",
	"stale_local_facts":                         "本地绑定或事实修订已改变",
	"stale_problem_local_facts":                 "问题引用的本地事实已失效",
	"stale_track_snapshot":                      "曲目快照不属于当前有效绑定",
	"stale_release_snapshot":                    "发行快照不属于当前有效绑定",
	"prefer_owned_compatible_track":             "未指定细发行，优先已收藏的兼容版本",
	"prefer_existing_compatible_release":        "未指定细发行，优先库内已有的兼容发行",
	"prefer_release_track_total":                "兼容发行中，优先已知总曲数更多的版本",
	"prefer_non_world_country":                  "总曲数相同，优先已知非世界发行国家",
	"prefer_stable_identity":                    "其余偏好相同，按稳定来源身份顺序选择",
	"exact_existing_track":                      "所选来源、曲目及发行与当前收藏精确一致",
	"automatic_target":                          "当前输入证据已确定可自动处理的精确目标",
}

// ExplainReasons never exposes a code or arbitrary source detail as the main
// user explanation. Unknown future codes retain a safe, readable fallback.
func ExplainReasons(reasons []DecisionReason) []DecisionReason {
	out := make([]DecisionReason, 0, len(reasons))
	seen := map[string]bool{}
	for _, reason := range reasons {
		key := reason.Code + "\x00" + reason.Field + "\x00" + reason.Detail
		if seen[key] {
			continue
		}
		seen[key] = true
		reason.Message = reasonMessages[reason.Code]
		if reason.Message == "" {
			reason.Message = "匹配证据需要进一步核验"
		}
		out = append(out, reason)
	}
	return out
}

func DecisionSummary(evidence *domain.DecisionEvidence) string {
	if evidence == nil {
		return ""
	}
	messages := []string{}
	seen := map[string]bool{}
	for _, r := range ExplainReasons(evidence.Reasons) {
		// Missing ancillary fields are diagnostic, not useful success summaries.
		if r.Code == "missing_text" || r.Code == "missing_number" {
			continue
		}
		if !seen[r.Message] {
			seen[r.Message] = true
			messages = append(messages, r.Message)
		}
	}
	return strings.Join(messages, "；")
}

// SuccessfulTargetReasons includes only coherent facets that actually support
// the selected target. Excluded competitors' conflicts must not be described
// as facts about the successful target. Review keeps the full pool reasons.
func SuccessfulTargetReasons(target CandidateTarget, input InputProfile) []DecisionReason {
	evidence := target.Check(input)
	if evidence.Eligibility != MetadataEligible {
		return nil
	}
	out := []DecisionReason{}
	for _, facet := range evidence.Facets {
		if facet.Eligibility == MetadataEligible {
			out = append(out, facet.Reasons...)
		}
	}
	return ExplainReasons(out)
}
