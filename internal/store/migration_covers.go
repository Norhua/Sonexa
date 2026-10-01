package store

// 封面目录、图片、规格、文件和用户选择分别记录。查询成功不代表图片已下载，
// 清除临时文件也不删除目录或用户选择。旧 cover_path 留给可恢复的文件迁移。
const migration0002CoverCatalog = `
CREATE TABLE cover_catalog (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL CHECK(source<>''),
  object_type TEXT NOT NULL,
  source_ref TEXT NOT NULL CHECK (source_ref <> ''),
  state TEXT NOT NULL DEFAULT 'unqueried' CHECK (state IN ('unqueried','found','empty','error')),
  checked_at DATETIME,
  actual_release_url TEXT NOT NULL DEFAULT '',
  last_error TEXT NOT NULL DEFAULT '',
  retry_after DATETIME,
  version INTEGER NOT NULL DEFAULT 0,
  UNIQUE (source, object_type, source_ref)
);
CREATE TABLE cover_image (
  id INTEGER PRIMARY KEY,
  catalog_id INTEGER NOT NULL REFERENCES cover_catalog(id),
  source_image_id TEXT NOT NULL CHECK (source_image_id <> ''),
  types_json TEXT NOT NULL DEFAULT '[]',
  primary_type TEXT NOT NULL DEFAULT 'Front',
  is_front INTEGER NOT NULL DEFAULT 0,
  is_back INTEGER NOT NULL DEFAULT 0,
  position INTEGER NOT NULL DEFAULT 0,
  original_url TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL DEFAULT '',
  artist TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1,
  UNIQUE (catalog_id, source_image_id)
);
CREATE TABLE cover_variant (
  id INTEGER PRIMARY KEY,
  image_id INTEGER NOT NULL REFERENCES cover_image(id),
  spec TEXT NOT NULL CHECK (spec <> ''),
  url TEXT NOT NULL DEFAULT '',
  constructed INTEGER NOT NULL DEFAULT 0,
  width INTEGER CHECK (width > 0),
  height INTEGER CHECK (height > 0),
  format TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL DEFAULT 'unverified' CHECK (state IN ('unverified','verified','unavailable','error')),
  last_error TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL DEFAULT 1,
  UNIQUE (image_id, spec),
  UNIQUE (id, image_id),
  CHECK ((width IS NULL) = (height IS NULL))
);
CREATE TABLE cover_file (
  id INTEGER PRIMARY KEY,
  variant_id INTEGER NOT NULL REFERENCES cover_variant(id),
  release_id INTEGER REFERENCES release(id) ON DELETE CASCADE,
  relative_path TEXT NOT NULL UNIQUE,
  content_hash TEXT NOT NULL,
  byte_size INTEGER NOT NULL CHECK (byte_size > 0),
  width INTEGER NOT NULL CHECK (width > 0),
  height INTEGER NOT NULL CHECK (height > 0),
  format TEXT NOT NULL,
  fetched_at DATETIME NOT NULL,
  last_used_at DATETIME NOT NULL,
  auto_reusable INTEGER NOT NULL DEFAULT 0 CHECK(auto_reusable IN (0,1)),
  protected INTEGER NOT NULL DEFAULT 0 CHECK (protected IN (0,1)),
  available INTEGER NOT NULL DEFAULT 1 CHECK (available IN (0,1)),
  CHECK (release_id IS NOT NULL OR protected = 0)
);
CREATE UNIQUE INDEX idx_cover_file_release ON cover_file(release_id, variant_id)
  WHERE release_id IS NOT NULL;
CREATE UNIQUE INDEX idx_cover_file_preview ON cover_file(variant_id)
  WHERE release_id IS NULL;
CREATE INDEX idx_cover_file_eviction ON cover_file(protected, available, last_used_at);
CREATE TABLE release_cover_selection (
  release_id INTEGER PRIMARY KEY REFERENCES release(id) ON DELETE CASCADE,
  image_id INTEGER REFERENCES cover_image(id),
  variant_id INTEGER,
  revision INTEGER NOT NULL CHECK (revision > 0),
  adopted_at DATETIME NOT NULL,
  FOREIGN KEY (variant_id, image_id) REFERENCES cover_variant(id, image_id)
);
`
