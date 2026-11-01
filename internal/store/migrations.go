package store

// 新代际仅用于空库初始化；当前值与上游事实独立保存。
const migration0001Init = `
CREATE TABLE model_generation (id INTEGER PRIMARY KEY CHECK(id=1), generation TEXT NOT NULL);
INSERT INTO model_generation VALUES(1,'track-identity-v6');
CREATE TABLE release_group (
 id INTEGER PRIMARY KEY, display_provider TEXT, display_group_ref TEXT, representative_id INTEGER,
 revision INTEGER NOT NULL DEFAULT 1, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
);
CREATE TABLE release_group_ref (
 group_id INTEGER NOT NULL REFERENCES release_group(id) ON DELETE CASCADE,
 provider TEXT NOT NULL, group_ref TEXT NOT NULL CHECK(group_ref<>''),
 PRIMARY KEY(provider,group_ref)
);
CREATE TABLE release_group_current (
 group_id INTEGER PRIMARY KEY REFERENCES release_group(id) ON DELETE CASCADE,
 title TEXT, credit TEXT, primary_type TEXT, secondary_types TEXT,
 states_json TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE release_group_snapshot (
 group_id INTEGER NOT NULL REFERENCES release_group(id) ON DELETE CASCADE,
 provider TEXT NOT NULL, group_ref TEXT NOT NULL, body TEXT NOT NULL CHECK(json_valid(body)),
 revision INTEGER NOT NULL DEFAULT 1, fetched_at DATETIME, release_count INTEGER,
 PRIMARY KEY(provider,group_ref)
);
CREATE TABLE release (
 id INTEGER PRIMARY KEY, release_group_id INTEGER NOT NULL REFERENCES release_group(id),
 provider TEXT, provider_ref TEXT, revision INTEGER NOT NULL DEFAULT 1,
 tracklist_state TEXT NOT NULL DEFAULT 'none', fetched_at DATETIME,
 created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
 CHECK((provider IS NULL)=(provider_ref IS NULL)), CHECK(provider_ref IS NULL OR provider_ref<>'')
);
CREATE UNIQUE INDEX idx_release_ref ON release(provider,provider_ref) WHERE provider_ref IS NOT NULL;
CREATE INDEX idx_release_group_of ON release(release_group_id);
CREATE TABLE release_current (
 release_id INTEGER PRIMARY KEY REFERENCES release(id) ON DELETE CASCADE,
 title TEXT, credit TEXT, primary_type TEXT, secondary_types TEXT,
 disc_total INTEGER CHECK(disc_total IS NULL OR disc_total>0), media_json TEXT,
 states_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_release_title ON release_current(title);
CREATE TABLE release_snapshot (
 release_id INTEGER PRIMARY KEY REFERENCES release(id) ON DELETE CASCADE,
 body TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(body)), revision INTEGER NOT NULL DEFAULT 1,
 fetched_at DATETIME
);
CREATE TABLE track (
 id INTEGER PRIMARY KEY, release_id INTEGER REFERENCES release(id) ON DELETE SET NULL,
 match_source TEXT, match_ref TEXT, matched_at DATETIME,
 revision INTEGER NOT NULL DEFAULT 1,
 enrich_state TEXT NOT NULL DEFAULT 'skipped', enrich_attempts INTEGER NOT NULL DEFAULT 0,
 enrich_error TEXT, credit_review TEXT,
 created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL,
 CHECK((match_source IS NULL)=(match_ref IS NULL)), CHECK(match_ref IS NULL OR match_ref<>'')
);
CREATE UNIQUE INDEX idx_match_ref ON track(match_source,match_ref) WHERE match_source IS NOT NULL;
CREATE INDEX idx_track_created ON track(created_at DESC);
CREATE INDEX idx_track_release ON track(release_id) WHERE release_id IS NOT NULL;
CREATE INDEX idx_track_enrich ON track(enrich_state);
CREATE TABLE lyrics_file (
 id INTEGER PRIMARY KEY, relative_path TEXT NOT NULL UNIQUE, content_hash TEXT NOT NULL,
 byte_size INTEGER NOT NULL, source TEXT NOT NULL, created_at DATETIME NOT NULL
);
CREATE TABLE track_current (
 track_id INTEGER PRIMARY KEY REFERENCES track(id) ON DELETE CASCADE,
 title TEXT, credit TEXT, year INTEGER CHECK(year IS NULL OR year>0),
 disc_number INTEGER CHECK(disc_number IS NULL OR disc_number>0),
 track_number INTEGER CHECK(track_number IS NULL OR track_number>0), genre TEXT,
 lyrics_file_id INTEGER REFERENCES lyrics_file(id), states_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX idx_track_title ON track_current(title);
CREATE TABLE track_snapshot (
 track_id INTEGER PRIMARY KEY REFERENCES track(id) ON DELETE CASCADE,
 body TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(body)), revision INTEGER NOT NULL DEFAULT 1,
	lyrics_file_id INTEGER REFERENCES lyrics_file(id), lyrics_state TEXT NOT NULL DEFAULT 'unknown' CHECK(lyrics_state IN ('unknown','empty','value')), fetched_at DATETIME
);
CREATE TABLE track_identifier (
 track_id INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
 kind TEXT NOT NULL, value TEXT NOT NULL, position INTEGER NOT NULL,
 PRIMARY KEY(track_id,kind,position)
);
CREATE INDEX idx_track_identifier ON track_identifier(kind,value);
CREATE TABLE release_group_match_key (
 group_id INTEGER NOT NULL REFERENCES release_group(id) ON DELETE CASCADE,
 source_kind TEXT NOT NULL, source_id TEXT NOT NULL, normalizer_version INTEGER NOT NULL,
 title_key TEXT NOT NULL CHECK(title_key<>''), credit_key TEXT NOT NULL CHECK(credit_key<>''),
 PRIMARY KEY(source_kind,source_id)
);
CREATE INDEX idx_group_match ON release_group_match_key(title_key,credit_key);
CREATE TABLE group_change_proposal (
 id INTEGER PRIMARY KEY, release_id INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
 evidence_json TEXT NOT NULL, evidence_hash TEXT NOT NULL DEFAULT '', baseline_revision INTEGER NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL
);
CREATE TABLE release_membership_decision (
 id INTEGER PRIMARY KEY, plan_id TEXT NOT NULL, content_hash TEXT NOT NULL,
 actions_json TEXT NOT NULL, evidence_json TEXT NOT NULL, approved_at DATETIME NOT NULL
);
CREATE TABLE operation_result (
 plan_id TEXT PRIMARY KEY, content_hash TEXT NOT NULL, result_json TEXT NOT NULL, committed_at DATETIME NOT NULL
);

CREATE VIEW release_group_read AS SELECT g.*, c.title,c.credit,c.primary_type,c.secondary_types,
 (SELECT provider FROM release_group_ref WHERE group_id=g.id ORDER BY provider,group_ref LIMIT 1) AS provider,
 (SELECT group_ref FROM release_group_ref WHERE group_id=g.id ORDER BY provider,group_ref LIMIT 1) AS provider_ref,
 (SELECT json_extract(body,'$.firstReleaseDate.value') FROM release_group_snapshot WHERE group_id=g.id ORDER BY provider,group_ref LIMIT 1) AS first_release_date,
 (SELECT release_count FROM release_group_snapshot WHERE group_id=g.id ORDER BY provider,group_ref LIMIT 1) AS release_count
 FROM release_group g JOIN release_group_current c ON c.group_id=g.id;
CREATE VIEW release_read AS SELECT r.*,COALESCE(c.title,'') AS title,c.credit,c.primary_type,c.secondary_types,c.disc_total,c.media_json,c.states_json,
 json_extract(s.body,'$.releaseTrackTotal.value') AS track_total,
 json_extract(s.body,'$.status.value') AS release_status,
 json_extract(s.body,'$.media.value[0].format.value') AS medium_format,
 json_extract(s.body,'$.country.value') AS country,
 json_extract(s.body,'$.date.value') AS release_date,
 (SELECT first_release_date FROM release_group_read WHERE id=r.release_group_id) AS first_release_date,
 json_extract(s.body,'$.labels.value[0].name') AS label,
 json_extract(s.body,'$.labels.value[0].catalogNumber') AS catalog_number,
 NULL AS cover_path,NULL AS cover_source_url,NULL AS cover_width,NULL AS cover_height,
 NULL AS cover_fetched_at,(SELECT json_group_object(key,json_extract(value,'$.source')) FROM json_each(c.states_json)) AS provenance_json,
 (SELECT json_group_object(key,json(CASE WHEN json_extract(value,'$.mode')='user' THEN 'true' ELSE 'false' END)) FROM json_each(c.states_json)) AS locked_json
 FROM release r JOIN release_current c ON c.release_id=r.id LEFT JOIN release_snapshot s ON s.release_id=r.id;
CREATE VIEW track_read AS SELECT t.*,COALESCE(c.title,'') AS title,c.credit,c.year,c.disc_number,c.track_number,c.genre,c.lyrics_file_id,c.states_json,
 json_extract(s.body,'$.trackNumberText.value') AS track_number_text,
 json_extract(s.body,'$.durationMS.value') AS duration_ms,
 json_extract(s.body,'$.recording.value.ref') AS recording_ref,
 json_extract(s.body,'$.works.value[0].ref') AS work_ref,
 (SELECT value FROM track_identifier WHERE track_id=t.id AND kind='ISRC' ORDER BY position LIMIT 1) AS isrc,
 NULL AS comment,NULL AS lyrics,(SELECT json_group_object(key,json_extract(value,'$.source')) FROM json_each(c.states_json)) AS provenance_json,
 (SELECT json_group_object(key,json(CASE WHEN json_extract(value,'$.mode')='user' THEN 'true' ELSE 'false' END)) FROM json_each(c.states_json)) AS locked_json
 FROM track t JOIN track_current c ON c.track_id=t.id LEFT JOIN track_snapshot s ON s.track_id=t.id;
CREATE TABLE artist (
  id             INTEGER PRIMARY KEY,
  name           TEXT NOT NULL,
  sort_name      TEXT,
  type           TEXT,
  disambiguation TEXT,
  mbid           TEXT UNIQUE,
  provider TEXT,
  provider_ref TEXT,
  created_at     DATETIME NOT NULL,
  updated_at     DATETIME NOT NULL
);
-- 名称用于检索；只有来源身份构成唯一约束，同名不同人可以并存。
CREATE INDEX idx_artist_name ON artist(name, COALESCE(disambiguation, ''));
CREATE UNIQUE INDEX idx_artist_ref ON artist(provider,provider_ref) WHERE provider_ref IS NOT NULL;

CREATE TABLE artist_link (
  from_artist_id INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  to_artist_id   INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  type           TEXT NOT NULL,
  PRIMARY KEY (from_artist_id, to_artist_id, type)
);
CREATE INDEX idx_artist_link_to ON artist_link(to_artist_id, type);

-- ========== 署名的分解：谁出现在这条署名串里 ==========
--
-- 它替代了此前的 credit_spans_json。区间本身仍是渲染辅助数据，
-- 但「这位艺术家是否被署名」必须可查询——艺术家列表的成员判定要用它，
-- 而 JSON 列做不到。
--
-- position 进主键：同一位艺术家可能在一条署名里出现两次
-- （形如 A feat. B & A），各自有各自的区间。
CREATE TABLE track_credit (
  track_id      INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
  artist_id     INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  position      INTEGER NOT NULL DEFAULT 0,
  credited_name TEXT,
  span_start    INTEGER,
  span_end      INTEGER,
  PRIMARY KEY (track_id, artist_id, position)
);
CREATE INDEX idx_track_credit_artist ON track_credit(artist_id);

-- 歌集署名的分解，与 track_credit 同构
CREATE TABLE release_credit (
  release_id    INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
  artist_id     INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  position      INTEGER NOT NULL DEFAULT 0,
  credited_name TEXT,
  span_start    INTEGER,
  span_end      INTEGER,
  PRIMARY KEY (release_id, artist_id, position)
);
CREATE INDEX idx_release_credit_artist ON release_credit(artist_id);

-- ========== 参与角色：完整艺术家 ==========
-- credited_name 不在这里——它归 track_credit。
CREATE TABLE track_artist (
  track_id  INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
  artist_id INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  role      TEXT NOT NULL,
  attribute TEXT,
  attributes_json TEXT NOT NULL DEFAULT '[]',
  position INTEGER NOT NULL DEFAULT 0,
  source    TEXT,
  PRIMARY KEY (track_id, artist_id, role, position)
);
CREATE INDEX idx_track_artist_lookup ON track_artist(artist_id, role);

-- ========== 歌单与标签：统一模型，kind 区分 ==========
CREATE TABLE collection (
  id         INTEGER PRIMARY KEY,
  name       TEXT NOT NULL,
  kind       TEXT NOT NULL,
  color      TEXT,
  sort_order INTEGER,
  created_at DATETIME NOT NULL
);
-- 唯一约束在种类内生效：同名的歌单与标签可以并存
CREATE UNIQUE INDEX idx_collection_name ON collection(kind, name);

CREATE TABLE collection_item (
  collection_id INTEGER NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
  track_id      INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
  position      INTEGER,
  added_at      DATETIME NOT NULL,
  PRIMARY KEY (collection_id, track_id)
);
CREATE INDEX idx_collection_item_track ON collection_item(track_id);

-- ========== 发行清单：上游事实的镜像 ==========
-- 与 track 之间仍无外键：清单位置与曲目的对应靠上游标识等值比对，
-- 删曲目不破坏清单，清单位置自然回落为灰色行。
CREATE TABLE release_track (
  release_id        INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
  disc_number       INTEGER NOT NULL DEFAULT 1,
  track_number      INTEGER NOT NULL,
  track_ref         TEXT,
  recording_ref     TEXT,
  title             TEXT,
  track_credit      TEXT,
  duration_ms       INTEGER,
  track_number_text TEXT,
  PRIMARY KEY (release_id, disc_number, track_number)
);
CREATE INDEX idx_release_track_ref ON release_track(track_ref)
  WHERE track_ref IS NOT NULL;

-- ========== 导入与复核队列 ==========
CREATE TABLE import_batch (
  id          INTEGER PRIMARY KEY,
  filename    TEXT NOT NULL,
  encoding    TEXT,
  delimiter   TEXT,
  column_map  TEXT,
  total_rows  INTEGER,
  status      TEXT,
  created_at  DATETIME NOT NULL,
  finished_at DATETIME
);

CREATE TABLE import_item (
  id             INTEGER PRIMARY KEY,
  batch_id       INTEGER NOT NULL REFERENCES import_batch(id) ON DELETE CASCADE,
  row_index      INTEGER NOT NULL,
  raw_row        TEXT NOT NULL,
  input_profile  TEXT CHECK(input_profile IS NULL OR json_valid(input_profile)),
  in_title       TEXT,
  in_artist      TEXT,
  in_album       TEXT,
  in_year        INTEGER,
  in_track_no    INTEGER,
  in_disc_no INTEGER,
  in_duration_ms INTEGER,
  revision       INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
  status         TEXT NOT NULL CHECK(status IN ('queued','matching','imported','review','failed','skipped','discarded')),
  review_reason  TEXT,
  review_evidence TEXT,
  attempt_count  INTEGER NOT NULL DEFAULT 0,
  last_error     TEXT,
  last_error_at  DATETIME,
  next_retry_at  DATETIME,
  candidates     TEXT,
  tried_variants TEXT,
  track_id       INTEGER,
  resolution_reason TEXT,
  resolution_target TEXT CHECK(resolution_target IS NULL OR json_valid(resolution_target)),
  resolution_problem_id INTEGER,
  resolution_problem_revision INTEGER CHECK(resolution_problem_revision IS NULL OR (typeof(resolution_problem_revision)='integer' AND resolution_problem_revision>0)),
  updated_at     DATETIME NOT NULL,
  CHECK((resolution_problem_id IS NULL)=(resolution_problem_revision IS NULL))
);
CREATE INDEX idx_import_item_queue ON import_item(status, next_retry_at);
CREATE INDEX idx_import_item_batch ON import_item(batch_id, row_index);

-- 原始输入与查询草稿分离；profile 可按规则重建，原文和解析值不可改写。
CREATE TRIGGER import_item_original_immutable
BEFORE UPDATE OF batch_id,row_index,raw_row,in_title,in_artist,in_album,in_year,in_track_no,in_disc_no,in_duration_ms ON import_item
WHEN NEW.batch_id IS NOT OLD.batch_id OR NEW.row_index IS NOT OLD.row_index
 OR NEW.raw_row IS NOT OLD.raw_row OR NEW.in_title IS NOT OLD.in_title
 OR NEW.in_artist IS NOT OLD.in_artist OR NEW.in_album IS NOT OLD.in_album
 OR NEW.in_year IS NOT OLD.in_year OR NEW.in_track_no IS NOT OLD.in_track_no
 OR NEW.in_disc_no IS NOT OLD.in_disc_no OR NEW.in_duration_ms IS NOT OLD.in_duration_ms
BEGIN SELECT RAISE(ABORT,'original import input is immutable'); END;

-- 活跃问题不是原始行，更不是可复用的历史选版映射。
CREATE TABLE review_problem (
  id INTEGER PRIMARY KEY,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','closed')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
  rule_version INTEGER NOT NULL CHECK(typeof(rule_version)='integer' AND rule_version>0),
  anchor_json TEXT NOT NULL CHECK(json_valid(anchor_json)),
  proof_json TEXT NOT NULL CHECK(json_valid(proof_json)),
  query_json TEXT CHECK(query_json IS NULL OR json_valid(query_json)),
  query_revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(query_revision)='integer' AND query_revision>0),
  candidate_facts TEXT CHECK(candidate_facts IS NULL OR json_valid(candidate_facts)),
  candidate_revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(candidate_revision)='integer' AND candidate_revision>0),
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  closed_at DATETIME,
  CHECK((status='closed')=(closed_at IS NOT NULL))
);
CREATE INDEX idx_review_problem_active ON review_problem(status,id);
CREATE TABLE review_problem_item (
  problem_id INTEGER NOT NULL REFERENCES review_problem(id) ON DELETE CASCADE,
  item_id INTEGER NOT NULL REFERENCES import_item(id) ON DELETE CASCADE,
  active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
  input_revision INTEGER NOT NULL CHECK(typeof(input_revision)='integer' AND input_revision>0),
  proof_json TEXT NOT NULL CHECK(json_valid(proof_json)),
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  PRIMARY KEY(problem_id,item_id)
);
CREATE UNIQUE INDEX idx_review_problem_item_active ON review_problem_item(item_id) WHERE active=1;
CREATE INDEX idx_review_problem_members ON review_problem_item(problem_id,active,item_id);
CREATE TRIGGER review_problem_member_insert
BEFORE INSERT ON review_problem_item WHEN NEW.active=1
BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM review_problem WHERE id=NEW.problem_id AND status='active')
  OR NOT EXISTS(SELECT 1 FROM import_item WHERE id=NEW.item_id AND status IN ('queued','matching','review','failed') AND revision=NEW.input_revision)
 THEN RAISE(ABORT,'review membership is stale') END;
END;
CREATE TRIGGER review_problem_member_update
BEFORE UPDATE ON review_problem_item WHEN NEW.active=1
BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM review_problem WHERE id=NEW.problem_id AND status='active')
  OR NOT EXISTS(SELECT 1 FROM import_item WHERE id=NEW.item_id AND status IN ('queued','matching','review','failed') AND revision=NEW.input_revision)
 THEN RAISE(ABORT,'review membership is stale') END;
END;
-- 生命周期事务须先移出成员，再结束行/关闭问题，不留下虚假的活跃归属。
CREATE TRIGGER review_problem_close_guard
BEFORE UPDATE OF status ON review_problem WHEN NEW.status='closed'
 AND EXISTS(SELECT 1 FROM review_problem_item WHERE problem_id=NEW.id AND active=1)
BEGIN SELECT RAISE(ABORT,'review problem still has active members'); END;
CREATE TRIGGER import_item_finish_guard
BEFORE UPDATE OF status ON import_item WHEN NEW.status IN ('imported','skipped','discarded')
 AND EXISTS(SELECT 1 FROM review_problem_item WHERE item_id=NEW.id AND active=1)
BEGIN SELECT RAISE(ABORT,'import item still has active membership'); END;

-- ========== 上游响应缓存：先落盘后解析 ==========
CREATE TABLE provider_cache (
  provider    TEXT NOT NULL,
  endpoint    TEXT NOT NULL,
  params_hash TEXT NOT NULL,
  body        BLOB NOT NULL,
  fetched_at  DATETIME NOT NULL,
  ttl_seconds INTEGER,
  PRIMARY KEY (provider, endpoint, params_hash)
);
CREATE INDEX idx_provider_cache_fetched ON provider_cache(fetched_at);

-- ========== 全文检索 ==========
-- 灰色行（未入库的清单曲目）不进索引：搜索的是用户的收藏，不是上游目录。
-- artist_names 是该曲目全部关联艺术家名的拼接——它让「搜索 平野綾」与
-- 「搜索 涼宮ハルヒ」都能命中同一首歌。两张关系表都要进：被署名的与
-- 担任角色的都应当能搜到。
CREATE VIEW track_fts_source AS
SELECT
  t.id AS rowid,
  COALESCE(t.title, '') AS title,
  COALESCE(t.credit, '') AS artist,
  COALESCE(r.title, '') AS album,
  COALESCE(t.credit, '') AS track_credit,
  COALESCE((
    SELECT group_concat(x.nm, ' ') FROM (
      SELECT DISTINCT COALESCE(tc.credited_name, a.name) AS nm
      FROM track_credit tc
      JOIN artist a ON a.id = tc.artist_id
      WHERE tc.track_id = t.id
      UNION
      SELECT DISTINCT a.name
      FROM track_credit tc
      JOIN artist a ON a.id = tc.artist_id
      WHERE tc.track_id = t.id
      UNION
      SELECT DISTINCT a.name
      FROM track_artist ta
      JOIN artist a ON a.id = ta.artist_id
      WHERE ta.track_id = t.id
      UNION
      SELECT COALESCE(rc.credited_name,a.name) FROM release_credit rc
      JOIN artist a ON a.id=rc.artist_id WHERE rc.release_id=t.release_id
      UNION
      SELECT c.credit FROM release_current c WHERE c.release_id=t.release_id AND c.credit IS NOT NULL
    ) x
  ), '') AS artist_names
FROM track_read t
LEFT JOIN release_read r ON r.id = t.release_id;

-- contentless 表：不存原文，只存倒排索引，省空间。
-- 删除索引行时 FTS5 要求提供「当初索引进去的那份值」，而数据随时会被改写，
-- 因此把那份值单独记在 track_fts_shadow 中，删除时以它为准。
CREATE VIRTUAL TABLE track_fts USING fts5(
  title,
  artist,
  album,
  track_credit,
  artist_names,
  content=''
);

-- 影子表：记录每个曲目当前索引进 track_fts 的那份值。
CREATE TABLE track_fts_shadow (
  track_id     INTEGER PRIMARY KEY,
  title        TEXT NOT NULL,
  artist       TEXT NOT NULL,
  album        TEXT NOT NULL,
  track_credit TEXT NOT NULL,
  artist_names TEXT NOT NULL
);
`
