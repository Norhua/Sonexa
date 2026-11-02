package store

// Confirmation relations are separate from raw inputs and the collection's
// primary provider binding. Changing that binding invalidates learned aliases.
const migration0003ImportEvidence = `
CREATE TABLE import_confirmation (
 item_id INTEGER PRIMARY KEY REFERENCES import_item(id) ON DELETE CASCADE,
 track_id INTEGER NOT NULL REFERENCES track(id) ON DELETE CASCADE,
 binding_key TEXT NOT NULL,
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
 created_at DATETIME NOT NULL
);
CREATE INDEX idx_import_confirmation_track ON import_confirmation(track_id);
CREATE TRIGGER invalidate_import_confirmation_binding AFTER UPDATE OF match_source,match_ref,release_id ON track
WHEN OLD.match_source IS NOT NEW.match_source OR OLD.match_ref IS NOT NEW.match_ref OR OLD.release_id IS NOT NEW.release_id
BEGIN DELETE FROM import_confirmation WHERE track_id=NEW.id; END;
CREATE TRIGGER invalidate_import_confirmation_position AFTER UPDATE OF disc_number,track_number ON track_current
WHEN OLD.disc_number IS NOT NEW.disc_number OR OLD.track_number IS NOT NEW.track_number
BEGIN DELETE FROM import_confirmation WHERE track_id=NEW.track_id; END;
CREATE TRIGGER invalidate_import_confirmation_snapshot_position AFTER UPDATE OF body ON track_snapshot
WHEN json_extract(OLD.body,'$.discNumber.value') IS NOT NULL AND json_extract(NEW.body,'$.discNumber.value') IS NOT json_extract(OLD.body,'$.discNumber.value')
 OR json_extract(OLD.body,'$.trackNumber.value') IS NOT NULL AND json_extract(NEW.body,'$.trackNumber.value') IS NOT json_extract(OLD.body,'$.trackNumber.value')
BEGIN DELETE FROM import_confirmation WHERE track_id=NEW.track_id; END;
CREATE TRIGGER invalidate_import_confirmation_release AFTER UPDATE OF provider,provider_ref ON release
WHEN OLD.provider IS NOT NEW.provider OR OLD.provider_ref IS NOT NEW.provider_ref
BEGIN DELETE FROM import_confirmation WHERE track_id IN (SELECT id FROM track WHERE release_id=NEW.id); END;
`
