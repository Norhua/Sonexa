package store

// Matching evidence is a disposable index, separate from display FTS. Dirty
// markers are written in the same transaction as facts, including raw SQL paths.
const migrationLocalRecall = `
CREATE TABLE local_match_state (
 id INTEGER PRIMARY KEY CHECK(id=1), rule_version INTEGER NOT NULL
);
INSERT INTO local_match_state VALUES(1,0);
CREATE TABLE local_match_dirty (
 track_id INTEGER PRIMARY KEY REFERENCES track(id) ON DELETE CASCADE
);
CREATE TABLE local_match_key (
 track_id INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
 field TEXT NOT NULL, source TEXT NOT NULL, name_key TEXT NOT NULL CHECK(name_key<>''),
 profile_json TEXT NOT NULL CHECK(json_valid(profile_json)), rule_version INTEGER NOT NULL,
 PRIMARY KEY(track_id,field,source,name_key)
);
CREATE INDEX idx_local_match_name ON local_match_key(field,name_key,track_id);
CREATE TRIGGER local_match_track_insert AFTER INSERT ON track BEGIN
 INSERT OR IGNORE INTO local_match_dirty VALUES(NEW.id);
END;
CREATE TRIGGER local_match_track_update AFTER UPDATE OF match_source,match_ref,release_id ON track BEGIN
 INSERT OR IGNORE INTO local_match_dirty VALUES(NEW.id);
END;
`

func localRecallSchema() string {
	sql := migrationLocalRecall
	// Facts on a track or its release invalidate all associated name keys.
	for _, entry := range []struct{ table, key, target string }{
		{"track_current", "track_id", "track"}, {"track_snapshot", "track_id", "track"},
		{"track_credit", "track_id", "track"}, {"track_artist", "track_id", "track"},
		{"release_current", "release_id", "release"}, {"release_snapshot", "release_id", "release"},
		{"release_credit", "release_id", "release"}, {"release", "id", "release"},
	} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			row := "NEW"
			if op == "DELETE" {
				row = "OLD"
			}
			where := "id=" + row + "." + entry.key
			if entry.target == "release" {
				where = "release_id=" + row + "." + entry.key
			}
			sql += "CREATE TRIGGER local_match_" + entry.table + "_" + op + " AFTER " + op + " ON " + entry.table + " BEGIN INSERT OR IGNORE INTO local_match_dirty SELECT id FROM track WHERE " + where + "; END;\n"
		}
	}
	// Artist names/links may be shared by multiple current credits and roles.
	// Mark those tracks, not the whole library; one-hop relations are only recall.
	for _, table := range []string{"artist", "artist_link"} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			row := "NEW"
			if op == "DELETE" {
				row = "OLD"
			}
			ids := row + ".id"
			if table == "artist_link" {
				ids = row + ".from_artist_id," + row + ".to_artist_id"
			}
			sql += "CREATE TRIGGER local_match_" + table + "_" + op + " AFTER " + op + " ON " + table + ` BEGIN
 INSERT OR IGNORE INTO local_match_dirty
 SELECT t.id FROM track t WHERE t.id IN (
  SELECT track_id FROM track_credit WHERE artist_id IN (` + ids + `)
  UNION SELECT track_id FROM track_artist WHERE artist_id IN (` + ids + `)
 ) OR t.release_id IN (SELECT release_id FROM release_credit WHERE artist_id IN (` + ids + `))
 OR t.id IN (
  SELECT track_id FROM track_credit WHERE artist_id IN (SELECT from_artist_id FROM artist_link WHERE to_artist_id IN (` + ids + `) UNION SELECT to_artist_id FROM artist_link WHERE from_artist_id IN (` + ids + `))
  UNION SELECT track_id FROM track_artist WHERE artist_id IN (SELECT from_artist_id FROM artist_link WHERE to_artist_id IN (` + ids + `) UNION SELECT to_artist_id FROM artist_link WHERE from_artist_id IN (` + ids + `))
 ) OR t.release_id IN (
  SELECT release_id FROM release_credit WHERE artist_id IN (SELECT from_artist_id FROM artist_link WHERE to_artist_id IN (` + ids + `) UNION SELECT to_artist_id FROM artist_link WHERE from_artist_id IN (` + ids + `))
 ); END;
`
		}
	}
	return sql
}
