-- A third kind of anomaly, a motor that draws a lot of power with a poor power factor, which points to a worn bearing or a winding that
-- is going bad. The power factor of the reading is kept with it, so the app can say how poor it was. Both changes are safe for the version
-- before this one, which only writes the two older kinds and never reads the new column.
SET LOCAL lock_timeout = '10s';

ALTER TABLE anomalies ADD COLUMN power_factor real;

ALTER TABLE anomalies DROP CONSTRAINT anomalies_kind;
ALTER TABLE anomalies ADD CONSTRAINT anomalies_kind CHECK (kind IN ('surge', 'overload', 'degradation')) NOT VALID;
ALTER TABLE anomalies VALIDATE CONSTRAINT anomalies_kind;

ALTER TABLE anomalies ADD CONSTRAINT anomalies_power_factor CHECK (power_factor IS NULL OR power_factor BETWEEN 0 AND 1) NOT VALID;
ALTER TABLE anomalies VALIDATE CONSTRAINT anomalies_power_factor;
