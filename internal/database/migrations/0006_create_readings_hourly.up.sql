-- One row for each node and each whole hour in UTC. It keeps the average and the highest and the lowest power, so a short spike stays
-- visible after the raw samples are deleted, and the energy of the hour. A day or a month is the sum of its hours in the time zone of the
-- business, which the service does when it reads. The rows are kept 400 days.
CREATE TABLE readings_hourly (
    gateway_id uuid NOT NULL,
    node_id text NOT NULL,
    hour timestamptz NOT NULL,
    samples integer NOT NULL,
    sum_watts double precision NOT NULL,
    max_watts real NOT NULL,
    min_watts real NOT NULL,
    energy_kwh double precision NOT NULL DEFAULT 0,
    PRIMARY KEY (gateway_id, node_id, hour),
    FOREIGN KEY (gateway_id, node_id) REFERENCES nodes (gateway_id, node_id) ON DELETE CASCADE,
    CONSTRAINT readings_hourly_samples CHECK (samples > 0),
    CONSTRAINT readings_hourly_hour CHECK (date_trunc('hour', hour, 'UTC') = hour),
    CONSTRAINT readings_hourly_values CHECK (sum_watts >= 0 AND energy_kwh >= 0 AND min_watts >= 0 AND min_watts <= max_watts AND max_watts <= 60000)
);

CREATE INDEX readings_hourly_hour_idx ON readings_hourly (hour);

-- every batch rewrites the row of the current hour of each node, and the indexed columns do not change, so free room on the page lets the
-- update stay on the page and keeps the table from bloating. The nodes table is updated on every batch as well.
ALTER TABLE readings_hourly SET (fillfactor = 70);
ALTER TABLE nodes SET (fillfactor = 70);
