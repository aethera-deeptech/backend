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
    CONSTRAINT readings_hourly_hour CHECK (date_trunc('hour', hour) = hour)
);

CREATE INDEX readings_hourly_hour_idx ON readings_hourly (hour);
