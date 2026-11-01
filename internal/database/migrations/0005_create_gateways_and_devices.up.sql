-- A gateway is the only unit that talks to the service. It sends its own key as a bearer token, and the service stores only the SHA-256
-- of that key, so a copy of the table cannot act as a gateway. The gateway makes the key itself and sends the hash when it enrolls.
CREATE TABLE gateways (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    name text NOT NULL,
    hardware_id text NOT NULL,
    key_hash bytea NOT NULL,
    -- the sequence number of the last batch that was stored, so a retry of a batch is stored once
    last_seq bigint NOT NULL DEFAULT 0,
    last_seen_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT gateways_name_length CHECK (char_length(name) BETWEEN 1 AND 40),
    CONSTRAINT gateways_key_hash_length CHECK (octet_length(key_hash) = 32),
    CONSTRAINT gateways_hardware_id_length CHECK (char_length(hardware_id) BETWEEN 6 AND 64)
);

CREATE UNIQUE INDEX gateways_hardware_id_key ON gateways (hardware_id);
CREATE UNIQUE INDEX gateways_key_hash_key ON gateways (key_hash);
CREATE INDEX gateways_business_id_idx ON gateways (business_id);

-- A gateway that asks to be claimed. The claim code and the poll id are stored as hashes, and both work once.
CREATE TABLE gateway_enrollments (
    poll_hash bytea PRIMARY KEY,
    code_hash bytea NOT NULL,
    hardware_id text NOT NULL,
    firmware text,
    key_hash bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    gateway_id uuid REFERENCES gateways (id) ON DELETE CASCADE,
    CONSTRAINT gateway_enrollments_hash_lengths CHECK (octet_length(poll_hash) = 32 AND octet_length(code_hash) = 32 AND octet_length(key_hash) = 32)
);

CREATE UNIQUE INDEX gateway_enrollments_code_hash_key ON gateway_enrollments (code_hash);
CREATE INDEX gateway_enrollments_expires_at_idx ON gateway_enrollments (expires_at);
CREATE INDEX gateway_enrollments_hardware_id_idx ON gateway_enrollments (hardware_id);

-- A node is a plug or a clamp that a gateway hears over ESP-NOW. The state columns hold the last sample, which the service needs to
-- turn a growing energy counter into the energy of each hour.
CREATE TABLE nodes (
    gateway_id uuid NOT NULL REFERENCES gateways (id) ON DELETE CASCADE,
    node_id text NOT NULL,
    kind text NOT NULL,
    hops smallint NOT NULL DEFAULT 0,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    last_sample_at timestamptz,
    last_energy_kwh double precision,
    PRIMARY KEY (gateway_id, node_id),
    CONSTRAINT nodes_kind CHECK (kind IN ('plug', 'clamp')),
    CONSTRAINT nodes_node_id_length CHECK (char_length(node_id) BETWEEN 6 AND 32)
);

-- A device is a node that a business claimed with a pairing
CREATE TABLE devices (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    gateway_id uuid NOT NULL,
    node_id text NOT NULL,
    kind text NOT NULL,
    name text NOT NULL,
    room_id uuid NOT NULL REFERENCES rooms (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (gateway_id, node_id) REFERENCES nodes (gateway_id, node_id) ON DELETE CASCADE,
    CONSTRAINT devices_name_length CHECK (char_length(name) BETWEEN 1 AND 40),
    CONSTRAINT devices_kind CHECK (kind IN ('plug', 'clamp'))
);

CREATE UNIQUE INDEX devices_node_key ON devices (gateway_id, node_id);
CREATE INDEX devices_business_id_idx ON devices (business_id);
CREATE INDEX devices_room_id_idx ON devices (room_id);

-- A search for a new device. A business has one running search at a time.
CREATE TABLE pairings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    kind text NOT NULL,
    name text NOT NULL,
    room_id uuid NOT NULL REFERENCES rooms (id),
    status text NOT NULL DEFAULT 'searching',
    device_id uuid REFERENCES devices (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    CONSTRAINT pairings_kind CHECK (kind IN ('plug', 'clamp')),
    CONSTRAINT pairings_status CHECK (status IN ('searching', 'connected', 'failed', 'cancelled'))
);

CREATE UNIQUE INDEX pairings_one_search_key ON pairings (business_id) WHERE status = 'searching';

-- The raw samples. Samples older than 30 days are deleted, and the hourly and daily summaries keep the history.
CREATE TABLE readings (
    gateway_id uuid NOT NULL,
    node_id text NOT NULL,
    t timestamptz NOT NULL,
    volts real,
    amps real,
    watts real NOT NULL,
    energy_kwh double precision,
    hertz real,
    power_factor real,
    node_state smallint,
    PRIMARY KEY (gateway_id, node_id, t),
    FOREIGN KEY (gateway_id, node_id) REFERENCES nodes (gateway_id, node_id) ON DELETE CASCADE,
    CONSTRAINT readings_watts_range CHECK (watts BETWEEN 0 AND 60000)
);

CREATE INDEX readings_t_idx ON readings (t);
