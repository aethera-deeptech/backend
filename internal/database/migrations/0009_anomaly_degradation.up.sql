-- A third kind of anomaly, a motor that draws a lot of power with a poor power factor, which points to a worn bearing or a winding that
-- is going bad. The power factor of the reading is kept with it, so the app can say how poor it was. Both changes work for the version
-- before this one, which only writes the two older kinds and never reads the new column.
SET LOCAL lock_timeout = '10s';

ALTER TABLE anomalies ADD COLUMN power_factor real;

-- The table has one row for each node and kind in 6 hours, so it is small and the check of the old rows is short. NOT VALID and VALIDATE
-- in one transaction hold the same lock as the plain constraint, so the plain form is used. The lock timeout above stops the migration
-- when a long query holds the table.
-- squawk-ignore ban-drop-constraint
ALTER TABLE anomalies DROP CONSTRAINT anomalies_kind;
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE anomalies ADD CONSTRAINT anomalies_kind CHECK (kind IN ('surge', 'overload', 'degradation'));

-- squawk-ignore constraint-missing-not-valid
ALTER TABLE anomalies ADD CONSTRAINT anomalies_power_factor CHECK (power_factor IS NULL OR power_factor BETWEEN 0 AND 1);
