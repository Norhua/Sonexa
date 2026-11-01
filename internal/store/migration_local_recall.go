package store

import "strings"

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
`

func localRecallSchema() string {
	schema := migrationLocalRecall
	// An outer UPSERT/OR ABORT can override a trigger's INSERT OR IGNORE.
	// Exclude existing markers instead: SQLite serializes writes, so repeated
	// invalidation cannot conflict, and we never suppress unrelated constraints.
	mark := func(where string) string {
		return `INSERT INTO local_match_dirty SELECT t.id FROM track t
 WHERE (` + where + `) AND NOT EXISTS(SELECT 1 FROM local_match_dirty d WHERE d.track_id=t.id);`
	}
	trigger := func(table, event, timing, where string) {
		name := strings.Fields(event)[0]
		schema += "CREATE TRIGGER local_match_" + table + "_" + name + " " + timing + " " + event + " ON " + table + " BEGIN " + mark(where) + " END;\n"
	}
	trigger("track", "INSERT", "AFTER", "t.id=NEW.id")
	trigger("track", "UPDATE OF match_source,match_ref,release_id", "AFTER", "t.id=NEW.id")
	// Moving a relation invalidates both its old and new owners. Other writes
	// usually keep the key unchanged; the set predicate still marks each once.
	for _, entry := range []struct{ table, key, target string }{
		{"track_current", "track_id", "track"}, {"track_snapshot", "track_id", "track"},
		{"track_credit", "track_id", "track"}, {"track_artist", "track_id", "track"},
		{"release_current", "release_id", "release"}, {"release_snapshot", "release_id", "release"},
		{"release_credit", "release_id", "release"}, {"release", "id", "release"},
	} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			var keys []string
			if op != "INSERT" {
				keys = append(keys, "OLD."+entry.key)
			}
			if op != "DELETE" {
				keys = append(keys, "NEW."+entry.key)
			}
			column := "t.id"
			if entry.target == "release" {
				column = "t.release_id"
			}
			trigger(entry.table, op, "AFTER", column+" IN ("+strings.Join(keys, ",")+")")
		}
	}
	// One-hop artist relationships are recall evidence, not identity aliases.
	// BEFORE DELETE retains edges before FK cascades remove them; updates mark
	// OLD and NEW endpoints, including tracks reached through release credits.
	for _, table := range []string{"artist", "artist_link"} {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			var keys []string
			for _, row := range []string{"OLD", "NEW"} {
				if (row == "OLD" && op == "INSERT") || (row == "NEW" && op == "DELETE") {
					continue
				}
				if table == "artist" {
					keys = append(keys, row+".id")
				} else {
					keys = append(keys, row+".from_artist_id", row+".to_artist_id")
				}
			}
			ids := strings.Join(keys, ",")
			related := `SELECT id FROM artist WHERE id IN (` + ids + `)
 UNION SELECT from_artist_id FROM artist_link WHERE to_artist_id IN (` + ids + `)
 UNION SELECT to_artist_id FROM artist_link WHERE from_artist_id IN (` + ids + `)`
			where := `t.id IN (SELECT track_id FROM track_credit WHERE artist_id IN (` + related + `)
 UNION SELECT track_id FROM track_artist WHERE artist_id IN (` + related + `))
 OR t.release_id IN (SELECT release_id FROM release_credit WHERE artist_id IN (` + related + `))`
			timing := "AFTER"
			if op == "DELETE" {
				timing = "BEFORE"
			}
			trigger(table, op, timing, where)
		}
	}
	return schema
}
