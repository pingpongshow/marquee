-- +goose Up
-- "Sonic analysis" is now Soundprint: rename its table, the stored setting and the task id.
ALTER TABLE sonic RENAME TO soundprint;

UPDATE settings
   SET value = json_remove(json_set(value, '$.music.soundprintAnalysis', json(CASE json_extract(value, '$.music.sonicAnalysis') WHEN 0 THEN 'false' ELSE 'true' END)), '$.music.sonicAnalysis')
 WHERE key = 'server_settings' AND json_valid(value) AND json_type(value, '$.music.sonicAnalysis') IS NOT NULL;

UPDATE task_runs SET task = 'soundprint' WHERE task = 'sonic';

-- +goose Down
ALTER TABLE soundprint RENAME TO sonic;

UPDATE settings
   SET value = json_remove(json_set(value, '$.music.sonicAnalysis', json(CASE json_extract(value, '$.music.soundprintAnalysis') WHEN 0 THEN 'false' ELSE 'true' END)), '$.music.soundprintAnalysis')
 WHERE key = 'server_settings' AND json_valid(value) AND json_type(value, '$.music.soundprintAnalysis') IS NOT NULL;

UPDATE task_runs SET task = 'sonic' WHERE task = 'soundprint';
