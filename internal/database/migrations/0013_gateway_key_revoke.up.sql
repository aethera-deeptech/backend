-- The owner can cut off the key of a gateway. The time of the cut is kept here and the key hash is replaced by a value that no key has, so
-- the old key gets 401. The column is new and nothing in the version before this one reads it, so both copies of a rolling update work.
SET LOCAL lock_timeout = '10s';

ALTER TABLE gateways ADD COLUMN key_revoked_at timestamptz;
