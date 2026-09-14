package store

// migration0001Init 建立 v1 的全部表与索引。
//
// 从第一天起就走迁移路径，而不是在代码里 CREATE TABLE IF NOT EXISTS——
// 否则迁移机制会因为"暂时用不上"而迟迟不落地，而 track_metadata 被明确
// 设计为可演进，没有迁移机制该设计就不成立。
const migration0001Init = `
-- ========== 基本信息：结构稳定，字段永不增删，每首曲目必有 ==========
CREATE TABLE track (
  id            INTEGER PRIMARY KEY,
  title         TEXT NOT NULL,
  artist        TEXT,
  album         TEXT,
  title_locked  INTEGER NOT NULL DEFAULT 0,
  artist_locked INTEGER NOT NULL DEFAULT 0,
  album_locked  INTEGER NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL,
  updated_at    DATETIME NOT NULL
);
CREATE INDEX idx_track_created ON track(created_at DESC);
CREATE INDEX idx_track_title ON track(title);

-- ========== 丰富信息：可演进，可整体为空 ==========
CREATE TABLE track_metadata (
  track_id           INTEGER PRIMARY KEY REFERENCES track(id) ON DELETE CASCADE,

  title              TEXT,
  album              TEXT,

  -- 署名原串：原样保真，不由关系表推导
  track_credit       TEXT,
  album_credit       TEXT,
  credit_spans_json  TEXT,

  -- 发行信息
  primary_type       TEXT,
  secondary_types    TEXT,
  release_status     TEXT,
  medium_format      TEXT,
  country            TEXT,
  year               INTEGER,
  release_date       TEXT,
  first_release_date TEXT,

  -- 位置与时长
  track_number       INTEGER,
  track_number_text  TEXT,
  track_total        INTEGER,
  disc_number        INTEGER,
  disc_total         INTEGER,
  duration_ms        INTEGER,

  -- 描述
  genre              TEXT,
  label              TEXT,
  catalog_number     TEXT,
  comment            TEXT,

  -- 封面：文件不入库
  cover_path         TEXT,
  cover_source       TEXT,
  cover_source_url   TEXT,
  cover_width        INTEGER,
  cover_height       INTEGER,
  cover_fetched_at   DATETIME,

  -- 上游标识（冗余存储，供跨上游桥接使用）
  recording_mbid       TEXT,
  release_mbid         TEXT,
  release_group_mbid   TEXT,
  work_mbid            TEXT,
  isrc                 TEXT,
  itunes_track_id      TEXT,
  itunes_collection_id TEXT,

  -- 匹配溯源
  match_source       TEXT,
  match_ref          TEXT,
  matched_at         DATETIME,

  -- 逐字段来源与用户锁
  provenance_json    TEXT,
  locked_json        TEXT,

  updated_at         DATETIME NOT NULL
);

-- 部分唯一索引：未匹配的曲目（match_source IS NULL）不受唯一约束
CREATE UNIQUE INDEX idx_match_ref ON track_metadata(match_source, match_ref)
  WHERE match_source IS NOT NULL;
CREATE INDEX idx_track_metadata_recording ON track_metadata(recording_mbid)
  WHERE recording_mbid IS NOT NULL;
CREATE INDEX idx_track_metadata_isrc ON track_metadata(isrc)
  WHERE isrc IS NOT NULL;
CREATE INDEX idx_track_metadata_rg ON track_metadata(release_group_mbid)
  WHERE release_group_mbid IS NOT NULL;

-- ========== 艺术家与角色关系 ==========
CREATE TABLE artist (
  id             INTEGER PRIMARY KEY,
  name           TEXT NOT NULL,
  sort_name      TEXT,
  type           TEXT,
  disambiguation TEXT,
  mbid           TEXT UNIQUE,
  created_at     DATETIME NOT NULL,
  updated_at     DATETIME NOT NULL
);
-- 同名 + 歧义消解的组合唯一：两位同名艺术家靠 disambiguation 区分，不被合并
CREATE UNIQUE INDEX idx_artist_name ON artist(name, COALESCE(disambiguation, ''));

CREATE TABLE artist_link (
  from_artist_id INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  to_artist_id   INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  type           TEXT NOT NULL,
  PRIMARY KEY (from_artist_id, to_artist_id, type)
);
CREATE INDEX idx_artist_link_to ON artist_link(to_artist_id, type);

CREATE TABLE track_artist (
  track_id      INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
  artist_id     INTEGER NOT NULL REFERENCES artist(id) ON DELETE CASCADE,
  role          TEXT NOT NULL,
  credited_name TEXT,
  attribute     TEXT,
  position      INTEGER,
  source        TEXT,
  PRIMARY KEY (track_id, artist_id, role)
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

-- ========== 发行清单：上游事实的镜像，与 track 之间无外键 ==========
CREATE TABLE release (
  id                 INTEGER PRIMARY KEY,
  provider           TEXT NOT NULL,
  provider_ref       TEXT NOT NULL,
  release_group_ref  TEXT,
  title              TEXT NOT NULL,
  album_credit       TEXT,
  primary_type       TEXT,
  secondary_types    TEXT,
  release_status     TEXT,
  medium_format      TEXT,
  country            TEXT,
  release_date       TEXT,
  first_release_date TEXT,
  track_total        INTEGER,
  disc_total         INTEGER,
  cover_path         TEXT,
  cover_source_url   TEXT,
  tracklist_state    TEXT NOT NULL,
  fetched_at         DATETIME,
  updated_at         DATETIME NOT NULL
);
CREATE UNIQUE INDEX idx_release_ref ON release(provider, provider_ref);
CREATE INDEX idx_release_group ON release(release_group_ref)
  WHERE release_group_ref IS NOT NULL;

CREATE TABLE release_track (
  release_id        INTEGER NOT NULL REFERENCES release(id) ON DELETE CASCADE,
  disc_number       INTEGER NOT NULL DEFAULT 1,
  track_number      INTEGER NOT NULL,
  track_ref         TEXT,
  recording_ref     TEXT,
  title             TEXT NOT NULL,
  track_credit      TEXT,
  duration_ms       INTEGER,
  track_number_text TEXT,
  PRIMARY KEY (release_id, disc_number, track_number)
);
CREATE INDEX idx_release_track_ref ON release_track(track_ref)
  WHERE track_ref IS NOT NULL;
CREATE INDEX idx_release_track_recording ON release_track(recording_ref)
  WHERE recording_ref IS NOT NULL;

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
  in_title       TEXT,
  in_artist      TEXT,
  in_album       TEXT,
  in_year        INTEGER,
  in_track_no    INTEGER,
  in_duration_ms INTEGER,
  status         TEXT NOT NULL,
  attempt_count  INTEGER NOT NULL DEFAULT 0,
  last_error     TEXT,
  last_error_at  DATETIME,
  next_retry_at  DATETIME,
  candidates     TEXT,
  tried_variants TEXT,
  track_id       INTEGER,
  merged_into    INTEGER,
  updated_at     DATETIME NOT NULL
);
CREATE INDEX idx_import_item_queue ON import_item(status, next_retry_at);
CREATE INDEX idx_import_item_batch ON import_item(batch_id, row_index);

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
-- FTS 的内容来源视图：把三处来源拼成索引行，使刷新逻辑只有一份。
-- artist_names 是该曲目全部关联艺术家名的拼接——它让「搜索 平野綾」与
-- 「搜索 涼宮ハルヒ」都能命中同一首歌。
CREATE VIEW track_fts_source AS
SELECT
  t.id AS rowid,
  t.title AS title,
  COALESCE(t.artist, '') AS artist,
  COALESCE(t.album, '') AS album,
  COALESCE(m.track_credit, '') AS track_credit,
  COALESCE((
    SELECT group_concat(x.nm, ' ') FROM (
      SELECT DISTINCT COALESCE(ta.credited_name, a.name) AS nm
      FROM track_artist ta
      JOIN artist a ON a.id = ta.artist_id
      WHERE ta.track_id = t.id
      UNION
      SELECT DISTINCT a.name
      FROM track_artist ta
      JOIN artist a ON a.id = ta.artist_id
      WHERE ta.track_id = t.id
    ) x
  ), '') AS artist_names
FROM track t
LEFT JOIN track_metadata m ON m.track_id = t.id;

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

// migration0002ReviewReason 为待复核条目记录成因。
//
// 复核队列里停着三种成因不同的条目：疑似重复、多义（跨发行组或同组多份发行）、
// 无结果。它们在导入统计里是两个互不重叠的数——「进入复核」与「未匹配」——
// 而仅凭 status='review' 分不出来，此前两个数因此永远相同。
const migration0002ReviewReason = `
ALTER TABLE import_item ADD COLUMN review_reason TEXT;
`
