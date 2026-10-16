-- +goose Up
-- DVR quotas, shared recordings and per-user DVR switches (LIVE-5).

-- The guide's content rating for a programme (XMLTV <rating>), carried onto recordings so
-- profiles with a rating limit see only what they may watch.
ALTER TABLE live_programmes ADD COLUMN rating TEXT NOT NULL DEFAULT '';
ALTER TABLE dvr_recordings ADD COLUMN rating TEXT NOT NULL DEFAULT '';
CREATE INDEX dvr_recordings_path ON dvr_recordings(path) WHERE path <> '';
CREATE INDEX dvr_recordings_user ON dvr_recordings(user_id, status);

-- The Recorded TV / Recorded Movies libraries (hidden from profiles that may not view DVR).
ALTER TABLE libraries ADD COLUMN recordings INTEGER NOT NULL DEFAULT 0;

-- Recording used to be opt-in ("canRecord", off unless granted); it is now on unless turned
-- off ("canRecordDVR": false). Profiles and invites that couldn't record keep not recording.
UPDATE users SET restrictions = json_set(json_remove(restrictions, '$.canRecord'), '$.canRecordDVR', json('false'))
WHERE is_admin = 0 AND json_valid(restrictions) AND COALESCE(json_extract(restrictions, '$.canRecord'), 0) = 0;
UPDATE users SET restrictions = json_remove(restrictions, '$.canRecord')
WHERE json_valid(restrictions) AND json_extract(restrictions, '$.canRecord') IS NOT NULL;
UPDATE invites SET restrictions = json_set(json_remove(restrictions, '$.canRecord'), '$.canRecordDVR', json('false'))
WHERE json_valid(restrictions) AND COALESCE(json_extract(restrictions, '$.canRecord'), 0) = 0;
UPDATE invites SET restrictions = json_remove(restrictions, '$.canRecord')
WHERE json_valid(restrictions) AND json_extract(restrictions, '$.canRecord') IS NOT NULL;

-- +goose Down
UPDATE users SET restrictions = json_set(json_remove(restrictions, '$.canRecordDVR'), '$.canRecord', json('true'))
WHERE json_valid(restrictions) AND json_extract(restrictions, '$.canRecordDVR') IS NULL AND is_admin = 0;
UPDATE users SET restrictions = json_remove(restrictions, '$.canRecordDVR', '$.canViewDVR', '$.dvrQuotaGB') WHERE json_valid(restrictions);
UPDATE invites SET restrictions = json_set(json_remove(restrictions, '$.canRecordDVR'), '$.canRecord', json('true'))
WHERE json_valid(restrictions) AND json_extract(restrictions, '$.canRecordDVR') IS NULL;
UPDATE invites SET restrictions = json_remove(restrictions, '$.canRecordDVR', '$.canViewDVR', '$.dvrQuotaGB') WHERE json_valid(restrictions);
ALTER TABLE libraries DROP COLUMN recordings;
DROP INDEX dvr_recordings_user;
DROP INDEX dvr_recordings_path;
ALTER TABLE dvr_recordings DROP COLUMN rating;
ALTER TABLE live_programmes DROP COLUMN rating;
