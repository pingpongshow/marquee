-- +goose Up
-- A recording stopped before its programme ended (Stop and keep) is marked as such.
ALTER TABLE dvr_recordings ADD COLUMN stopped_early INTEGER NOT NULL DEFAULT 0;
-- Channel groups no longer limit anyone: the stored lists go.
UPDATE users SET restrictions = json_remove(restrictions, '$.liveTvGroups')
  WHERE json_valid(restrictions) AND json_type(restrictions, '$.liveTvGroups') IS NOT NULL;
UPDATE invites SET restrictions = json_remove(restrictions, '$.liveTvGroups')
  WHERE json_valid(restrictions) AND json_type(restrictions, '$.liveTvGroups') IS NOT NULL;

-- +goose Down
ALTER TABLE dvr_recordings DROP COLUMN stopped_early;
