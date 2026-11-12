package store

// releaseDefinitionSchema 是修改 4（发行定义）的新表。
//
// 三张表都不重写上游事实，只记本地判断：
//   - release_coexistence：用户输入 `Yes` 明确允许并存的发行对，归并提议据此排除。
//   - release_merge_proposal：清单后到才发现曲目相同的两个发行，只提议不自动改。
//   - release_group_separation：两个本地发行组「不是同一组」的否定决定（按组对）。
func releaseDefinitionSchema() string {
	return `
CREATE TABLE release_coexistence (
 release_a INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
 release_b INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
 created_at DATETIME NOT NULL,
 PRIMARY KEY(release_a,release_b), CHECK(release_a<release_b)
);
CREATE TABLE release_merge_proposal (
 id INTEGER PRIMARY KEY,
 release_a INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
 release_b INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
 evidence_hash TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','dismissed')),
 created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
 UNIQUE(release_a,release_b,evidence_hash), CHECK(release_a<release_b)
);
CREATE TABLE release_group_separation (
 group_a INTEGER NOT NULL REFERENCES release_group(id) ON DELETE CASCADE,
 group_b INTEGER NOT NULL REFERENCES release_group(id) ON DELETE CASCADE,
 created_at DATETIME NOT NULL,
 PRIMARY KEY(group_a,group_b), CHECK(group_a<group_b)
);
`
}
