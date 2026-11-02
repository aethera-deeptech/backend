-- Relay control of a plug. The owner sends a command or sets a schedule, the service keeps the command until the gateway of the plug
-- fetches it in the answer to its next batch and reports the result in a later batch. A command that nobody fetched in time expires, so
-- a plug is never switched by an order that is hours old. The nodes keep the state of the relay that the plug last reported.
SET LOCAL lock_timeout = '10s';

ALTER TABLE nodes ADD COLUMN relay_on boolean;
ALTER TABLE nodes ADD COLUMN relay_at timestamptz;

CREATE TABLE relay_commands (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    gateway_id uuid NOT NULL REFERENCES gateways (id) ON DELETE CASCADE,
    node_id text NOT NULL,
    action text NOT NULL,
    -- owner is a command of the owner in the app, and schedule is one that the schedule of the device made
    source text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    sent_at timestamptz,
    done_at timestamptz,
    CONSTRAINT relay_commands_action CHECK (action IN ('on', 'off')),
    CONSTRAINT relay_commands_source CHECK (source IN ('owner', 'schedule')),
    CONSTRAINT relay_commands_status CHECK (status IN ('pending', 'sent', 'applied', 'failed', 'expired'))
);

CREATE INDEX relay_commands_gateway_idx ON relay_commands (gateway_id, created_at);
-- a device has at most one command that is open, so a double tap or a schedule that meets the owner can never leave two orders
CREATE UNIQUE INDEX relay_commands_device_open_key ON relay_commands (device_id) WHERE status IN ('pending', 'sent');
CREATE INDEX relay_commands_device_idx ON relay_commands (device_id, created_at DESC);
CREATE INDEX relay_commands_created_idx ON relay_commands (created_at);

-- One schedule for each device. The times are local times of the business, off_at switches the plug off and on_at switches it on again,
-- and checked_at is the moment up to which the service has looked for a time that passed.
CREATE TABLE relay_schedules (
    device_id uuid PRIMARY KEY REFERENCES devices (id) ON DELETE CASCADE,
    off_at time NOT NULL,
    on_at time,
    days text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    checked_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT relay_schedules_days CHECK (days IN ('mon-fri', 'mon-sat', 'daily')),
    CONSTRAINT relay_schedules_times CHECK (on_at IS NULL OR on_at <> off_at)
);
