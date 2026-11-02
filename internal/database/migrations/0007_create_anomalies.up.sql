-- A power reading that is far above what the device normally draws at that hour. The service writes a row when it sees one, and a device
-- counts as an anomaly for 24 hours after the newest row, because the owner wants to see on the next screen what happened today.
CREATE TABLE anomalies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    gateway_id uuid NOT NULL,
    node_id text NOT NULL,
    at timestamptz NOT NULL,
    peak_watts real NOT NULL,
    expected_high_watts real,
    -- surge is a reading far above the usual power at that hour, and overload is the code of the node itself
    kind text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (gateway_id, node_id) REFERENCES nodes (gateway_id, node_id) ON DELETE CASCADE,
    CONSTRAINT anomalies_kind CHECK (kind IN ('surge', 'overload')),
    CONSTRAINT anomalies_peak CHECK (peak_watts BETWEEN 0 AND 60000)
);

CREATE INDEX anomalies_node_at_idx ON anomalies (gateway_id, node_id, at DESC);
