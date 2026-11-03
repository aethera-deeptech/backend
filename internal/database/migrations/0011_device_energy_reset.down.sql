SET LOCAL lock_timeout = '10s';

ALTER TABLE devices DROP COLUMN energy_reset_at;
