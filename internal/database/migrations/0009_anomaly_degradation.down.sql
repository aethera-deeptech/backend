SET LOCAL lock_timeout = '10s';

-- the older constraint has no place for the new kind, so those rows go
DELETE FROM anomalies WHERE kind = 'degradation';

ALTER TABLE anomalies DROP CONSTRAINT anomalies_power_factor;
ALTER TABLE anomalies DROP CONSTRAINT anomalies_kind;
ALTER TABLE anomalies ADD CONSTRAINT anomalies_kind CHECK (kind IN ('surge', 'overload')) NOT VALID;
ALTER TABLE anomalies VALIDATE CONSTRAINT anomalies_kind;
ALTER TABLE anomalies DROP COLUMN power_factor;
