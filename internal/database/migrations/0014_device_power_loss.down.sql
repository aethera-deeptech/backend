SET LOCAL lock_timeout = '10s';

ALTER TABLE devices DROP CONSTRAINT devices_after_power_loss;
ALTER TABLE devices DROP COLUMN after_power_loss;
