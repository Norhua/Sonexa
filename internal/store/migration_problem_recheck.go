package store

import (
	"fmt"
	"strings"
)

const problemRecheckSchema = `
CREATE TABLE review_library_state(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL);
INSERT INTO review_library_state VALUES(1,1);
CREATE TABLE review_library_dirty(track_id INTEGER PRIMARY KEY,revision INTEGER NOT NULL DEFAULT 1,after_problem INTEGER NOT NULL DEFAULT 0);
-- Prior facts survive deletion/rebinding and repeated events are coalesced.
CREATE TABLE review_library_before(track_id INTEGER NOT NULL,body TEXT NOT NULL,PRIMARY KEY(track_id,body));
CREATE TABLE review_problem_network(batch_id INTEGER PRIMARY KEY REFERENCES import_batch(id) ON DELETE CASCADE);
CREATE TABLE review_problem_key(
 problem_id INTEGER NOT NULL REFERENCES review_problem(id) ON DELETE CASCADE,
 kind TEXT NOT NULL,value TEXT NOT NULL,prefix INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(problem_id,kind,value,prefix)
);
CREATE INDEX idx_review_problem_key ON review_problem_key(kind,value,problem_id);
ALTER TABLE review_problem_recheck ADD COLUMN after_item INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_problem_recheck ADD COLUMN last_error TEXT;
ALTER TABLE review_problem_recheck ADD COLUMN retry_at INTEGER NOT NULL DEFAULT 0;
CREATE TRIGGER review_problem_finished AFTER UPDATE OF status ON review_problem WHEN NEW.status='closed'
BEGIN
 DELETE FROM review_problem_key WHERE problem_id=NEW.id;
 DELETE FROM review_problem_recheck WHERE problem_id=NEW.id;
END;
`

// Group membership/native mappings affect edition preference even without a
// change to a track's title or binding. Mark all tracks in the affected groups.
func problemGroupTriggers() string {
	var schema string
	for _, table := range []string{"release_group", "release_group_ref", "release_group_current", "release_group_snapshot"} {
		column := "group_id"
		if table == "release_group" {
			column = "id"
		}
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			var keys []string
			if op != "INSERT" {
				keys = append(keys, "OLD."+column)
			}
			if op != "DELETE" {
				keys = append(keys, "NEW."+column)
			}
			where := "t.release_id IN (SELECT id FROM release WHERE release_group_id IN (" + strings.Join(keys, ",") + "))"
			timing := "AFTER"
			if op == "DELETE" {
				timing = "BEFORE"
			}
			schema += fmt.Sprintf("CREATE TRIGGER review_library_%s_%s %s %s ON %s BEGIN %s END;\n", table, op, timing, op, table, problemLibraryMark(where))
		}
	}
	return schema
}

// Reuse the local evidence invalidation boundary, including relation and source
// snapshot changes. BEFORE captures the last view before FK cascades remove it.
func problemLibraryMark(where string) string {
	before := `json_object('title',c.title,'artist',c.credit,'album',rc.title,'albumArtist',rc.credit,
 'source',t.match_source,'ref',t.match_ref,'releaseSource',r.provider,'releaseRef',r.provider_ref,
 'trackSnapshot',ts.body,'releaseSnapshot',rs.body,'relationNames',json((
 SELECT json_group_array(name) FROM (
 SELECT a.name FROM track_credit x JOIN artist a ON a.id=x.artist_id WHERE x.track_id=t.id
 UNION SELECT x.credited_name FROM track_credit x WHERE x.track_id=t.id
 UNION SELECT a.name FROM track_artist x JOIN artist a ON a.id=x.artist_id WHERE x.track_id=t.id
 UNION SELECT a.name FROM release_credit x JOIN artist a ON a.id=x.artist_id WHERE x.release_id=t.release_id
 UNION SELECT x.credited_name FROM release_credit x WHERE x.release_id=t.release_id
 UNION SELECT a.name FROM artist_link l JOIN artist a ON a.id=l.to_artist_id
 WHERE l.from_artist_id IN (SELECT artist_id FROM track_credit WHERE track_id=t.id UNION SELECT artist_id FROM track_artist WHERE track_id=t.id)
 UNION SELECT a.name FROM artist_link l JOIN artist a ON a.id=l.from_artist_id
 WHERE l.to_artist_id IN (SELECT artist_id FROM track_credit WHERE track_id=t.id UNION SELECT artist_id FROM track_artist WHERE track_id=t.id)
 ))))`
	return `INSERT INTO review_library_dirty(track_id) SELECT t.id FROM track t WHERE (` + where + `)
 AND NOT EXISTS(SELECT 1 FROM review_library_dirty d WHERE d.track_id=t.id);
 UPDATE review_library_dirty SET revision=revision+1,after_problem=0 WHERE track_id IN (SELECT t.id FROM track t WHERE ` + where + `);
 UPDATE review_library_state SET revision=revision+1 WHERE id=1;
 INSERT INTO review_library_before(track_id,body)
 SELECT t.id,` + before + `
 FROM track t LEFT JOIN track_current c ON c.track_id=t.id LEFT JOIN release r ON r.id=t.release_id
 LEFT JOIN release_current rc ON rc.release_id=r.id LEFT JOIN track_snapshot ts ON ts.track_id=t.id
 LEFT JOIN release_snapshot rs ON rs.release_id=r.id WHERE (` + where + `)
 AND EXISTS(SELECT 1 FROM review_problem WHERE status='active')
 AND NOT EXISTS(SELECT 1 FROM review_library_before b WHERE b.track_id=t.id AND b.body=` + before + `);`
}
