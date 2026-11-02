package store

// Existing records have no accepted target baseline. Do not manufacture one
// from today's potentially edited data; these keep the conservative v2 check.
const migration0004ConfirmationContext = `
ALTER TABLE import_confirmation ADD COLUMN target_context TEXT CHECK(target_context IS NULL OR json_valid(target_context));
DROP TRIGGER invalidate_import_confirmation_position;
CREATE TRIGGER invalidate_import_confirmation_position AFTER UPDATE OF disc_number,track_number ON track_current
WHEN (OLD.disc_number IS NOT NULL AND OLD.disc_number IS NOT NEW.disc_number)
 OR (OLD.track_number IS NOT NULL AND OLD.track_number IS NOT NEW.track_number)
BEGIN DELETE FROM import_confirmation WHERE track_id=NEW.track_id; END;
`
