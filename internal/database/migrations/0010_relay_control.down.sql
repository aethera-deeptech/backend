SET LOCAL lock_timeout = '10s';

DROP TABLE relay_schedules;
DROP TABLE relay_commands;
ALTER TABLE nodes DROP COLUMN relay_at;
ALTER TABLE nodes DROP COLUMN relay_on;
