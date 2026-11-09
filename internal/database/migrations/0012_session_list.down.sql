SET LOCAL lock_timeout = '10s';

ALTER TABLE sessions DROP CONSTRAINT sessions_label_length;
ALTER TABLE sessions DROP COLUMN label;
ALTER TABLE sessions DROP COLUMN id;
