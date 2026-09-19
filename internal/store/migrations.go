package store

// migration0001Init 建立 v1 的全部表与索引。
//
// 从第一天起就走迁移路径，而不是在代码里 CREATE TABLE IF NOT EXISTS——
// 否则迁移机制会因为「暂时用不上」而迟迟不落地。
//
// # 本版结构的三条主线
//
// 一、**发行（歌集）是实体，曲目通过外键指向它。** 此前发行级事实
// （专辑名、类型、载体、国家、年份、碟数、音轨数、厂牌、目录号、封面）
// 逐曲目复制一份存在 track_metadata 上，一张 12 首的专辑要存 12 份，
// 改其中一份另外 11 份不动。现在它们只存在 release 上，曲目只保留
// 自己那一份事实（碟号、音轨号、时长、风格、注释）。
//
// 二、**「署名」与「参与角色」是两件事。** track_credit 记录「谁出现在这条
// 署名里」，track_artist 记录「谁在这首歌上担任什么角色」。此前只有后者，
// 于是未署名的作词作曲与署名艺术家在库里长得一模一样，艺术家列表无从区分。
//
// 三、**曲目是单表。** 此前 track 与 track_metadata 各存一份曲名、专辑名、
// 署名，三对近似重复，写入路径两边都写而「哪一份为准」没有定论。
// 「基本信息必存在」由 title NOT NULL 表达，「丰富信息可整体为空」由
// 其余列可空表达——语义不丢，漂移的可能消失。
const migration0001Init = `
-- ========== 发行组：同一专辑的不同版本合为一条 ==========
CREATE TABLE release_group (
  id                 INTEGER PRIMARY KEY,
  provider           TEXT,
  provider_ref       TEXT,
  title              TEXT NOT NULL,
  primary_type       TEXT,
  secondary_types    TEXT,
  first_release_date TEXT,

  -- 该组下共有几个 release。取自上游 browse 请求（本地只见过其中几个，
  -- 数本地行数得到的是「我见过几个」而不是「上游共有几个」），可手动改。
  release_count            INTEGER,
  release_count_fetched_at DATETIME,

  created_at         DATETIME NOT NULL,
  updated_at         DATETIME NOT NULL
);
CREATE UNIQUE INDEX idx_release_group_ref ON release_group(provider, provider_ref)
  WHERE provider_ref IS NOT NULL;

-- ========== 歌集：专辑 / EP / 单曲 / Broadcast / Other ==========
-- provider 与 provider_ref 可空：本地新建的歌集（用户改歌集名新建、
-- 无上游标识的曲目归拢）两者都为 NULL。
CREATE TABLE release (
  id                INTEGER PRIMARY KEY,
  release_group_id  INTEGER REFERENCES release_group(id) ON DELETE SET NULL,
  provider          TEXT,
  provider_ref      TEXT,

  title             TEXT NOT NULL,
  credit            TEXT,
  primary_type      TEXT,
  secondary_types   TEXT,
  disc_total        INTEGER,
  track_total       INTEGER,

  -- 封面：文件不入库，只记落盘路径与取图来源。
  -- 挂在歌集上而不是曲目上——采用一次即对该歌集全部曲目生效。
  cover_path        TEXT,
  cover_source_url  TEXT,
  cover_width       INTEGER,
  cover_height      INTEGER,
  cover_fetched_at  DATETIME,

  -- 上游事实，不进编辑界面
  release_status    TEXT,
  medium_format     TEXT,
  country           TEXT,
  release_date      TEXT,
  first_release_date TEXT,
  label             TEXT,
  catalog_number    TEXT,

  tracklist_state   TEXT NOT NULL DEFAULT 'none',
  fetched_at        DATETIME,

  provenance_json   TEXT,
  locked_json       TEXT,
  created_at        DATETIME NOT NULL,
  updated_at        DATETIME NOT NULL
);
-- 唯一性只对有上游标识的歌集成立。title 上没有唯一约束：
-- 不同版次、不同艺术家的同名歌集是常态。
CREATE UNIQUE INDEX idx_release_ref ON release(provider, provider_ref)
  WHERE provider_ref IS NOT NULL;
CREATE INDEX idx_release_group_of ON release(release_group_id)
  WHERE release_group_id IS NOT NULL;
CREATE INDEX idx_release_title ON release(title);

-- ========== 曲目：单表，基本信息与丰富信息合并 ==========
CREATE TABLE track (
  id            INTEGER PRIMARY KEY,
  -- 删歌集不删曲目：曲目是用户的收藏，歌集是它的归属。
  release_id    INTEGER REFERENCES release(id) ON DELETE SET NULL,

  title         TEXT NOT NULL,
  -- 曲目级署名原串：原样保真，不由 track_credit 拼接推导。
  credit        TEXT,

  disc_number       INTEGER,
  track_number      INTEGER,
  track_number_text TEXT,
  duration_ms       INTEGER,
  genre             TEXT,
  comment           TEXT,

  -- 匹配溯源与上游标识
  match_source  TEXT,
  match_ref     TEXT,
  matched_at    DATETIME,
  recording_ref TEXT,
  work_ref      TEXT,
  isrc          TEXT,

  -- 补全信息匹配的状态机
  enrich_state    TEXT NOT NULL DEFAULT 'skipped',
  enrich_attempts INTEGER NOT NULL DEFAULT 0,
  enrich_error    TEXT,

  -- 逐字段来源与用户锁：整体读写的结构，新增字段无需改动它们的存储形状。
  -- 基本信息的三个布尔锁并入 locked_json——合表后没有理由留两套锁机制。
  provenance_json TEXT,
  locked_json     TEXT,

  created_at    DATETIME NOT NULL,
  updated_at    DATETIME NOT NULL
);
CREATE INDEX idx_track_created ON track(created_at DESC);
CREATE INDEX idx_track_title ON track(title);
CREATE INDEX idx_track_release ON track(release_id) WHERE release_id IS NOT NULL;
-- 部分唯一索引：未匹配的曲目（match_source IS NULL）不受唯一约束
CREATE UNIQUE INDEX idx_match_ref ON track(match_source, match_ref)
  WHERE match_source IS NOT NULL;
CREATE INDEX idx_track_isrc ON track(isrc) WHERE isrc IS NOT NULL;
CREATE INDEX idx_track_enrich ON track(enrich_state);

-- ========== 艺术家与关系 ==========
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
  position  INTEGER,
  source    TEXT,
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

-- ========== 发行清单：上游事实的镜像 ==========
-- 与 track 之间仍无外键：清单位置与曲目的对应靠上游标识等值比对，
-- 删曲目不破坏清单，清单位置自然回落为灰色行。
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
  review_reason  TEXT,
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
-- artist_names 是该曲目全部关联艺术家名的拼接——它让「搜索 平野綾」与
-- 「搜索 涼宮ハルヒ」都能命中同一首歌。两张关系表都要进：被署名的与
-- 担任角色的都应当能搜到。
CREATE VIEW track_fts_source AS
SELECT
  t.id AS rowid,
  t.title AS title,
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
    ) x
  ), '') AS artist_names
FROM track t
LEFT JOIN release r ON r.id = t.release_id;

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
