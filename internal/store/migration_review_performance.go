package store

import "fmt"

// Revisions are transactional and shared by all connections/Store instances.
// Cache/provider/cover writes deliberately do not invalidate import evidence.
func reviewPerformanceSchema() string {
	sql := `
ALTER TABLE import_item ADD COLUMN review_entered INTEGER NOT NULL DEFAULT 0 CHECK(review_entered IN (0,1));
ALTER TABLE import_item ADD COLUMN processed_once INTEGER NOT NULL DEFAULT 0 CHECK(processed_once IN (0,1));
CREATE TABLE review_revision(id INTEGER PRIMARY KEY CHECK(id=1),revision INTEGER NOT NULL);
INSERT INTO review_revision VALUES(1,1);
CREATE TRIGGER import_review_history AFTER UPDATE OF status,merged_item_id ON import_item
BEGIN
 UPDATE import_item SET processed_once=1 WHERE id=NEW.id AND NEW.status IN ('imported','review','failed','skipped','merged','discarded');
 UPDATE import_item SET review_entered=1,processed_once=1
 WHERE (id=NEW.id OR merged_item_id=NEW.id) AND NEW.status IN ('review','failed')
 AND EXISTS(SELECT 1 FROM import_destination d WHERE d.input_id=import_item.id AND d.state='active');
 UPDATE import_item SET review_entered=1,processed_once=1 WHERE id=NEW.id AND NEW.status='merged'
 AND EXISTS(SELECT 1 FROM import_item shell WHERE shell.id=NEW.merged_item_id AND shell.status IN ('review','failed'))
 AND EXISTS(SELECT 1 FROM import_destination d WHERE d.input_id=NEW.id AND d.state='active');
END;
CREATE TRIGGER import_cancel_history AFTER UPDATE OF state ON import_destination WHEN NEW.state='cancelled'
BEGIN
 UPDATE import_item SET processed_once=1 WHERE id=NEW.input_id;
END;
`
	for _, table := range []string{"track", "track_current", "track_snapshot", "release", "release_current", "release_snapshot", "import_item", "import_confirmation", "import_destination"} {
		for _, action := range []string{"INSERT", "UPDATE", "DELETE"} {
			sql += fmt.Sprintf("CREATE TRIGGER review_revision_%s_%s AFTER %s ON %s BEGIN UPDATE review_revision SET revision=revision+1 WHERE id=1; END;\n", table, action, action, table)
		}
	}
	return sql
}
