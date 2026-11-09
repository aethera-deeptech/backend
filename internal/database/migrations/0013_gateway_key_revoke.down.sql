SET LOCAL lock_timeout = '10s';

ALTER TABLE gateways DROP COLUMN key_revoked_at;
