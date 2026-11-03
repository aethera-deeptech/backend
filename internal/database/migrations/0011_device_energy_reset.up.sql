-- The owner can start a new total of the energy of a device. The time of the last start is kept here, and the total is the sum of the
-- hourly rows from that time. No reading is deleted. The column is new and nothing in the version before this one reads it, so both
-- copies of a rolling update work.
SET LOCAL lock_timeout = '10s';

ALTER TABLE devices ADD COLUMN energy_reset_at timestamptz;
