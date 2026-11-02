-- A tip for one device. The service makes it from the readings, the owner decides on it, and after a decision to apply it the service
-- compares the energy for each piece before and after. There is one tip for each device and kind for good, so a rejected tip does not
-- come back and a verified one is not suggested again.
CREATE TABLE tips (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    device_id uuid NOT NULL REFERENCES devices (id) ON DELETE CASCADE,
    -- after-hours is a device that draws working power when the work is over, standby is one that draws only its standby power
    kind text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    -- the energy that the tip saves in 30 days, which the service works out again while the tip waits
    saving_kwh_month double precision NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    applied_at timestamptz,
    rejected_at timestamptz,
    verified_at timestamptz,
    -- the result of the check, in kWh for each piece, and the saving that it measured in 30 days
    before_kwh_per_piece double precision,
    after_kwh_per_piece double precision,
    measured_kwh_month double precision,
    CONSTRAINT tips_kind CHECK (kind IN ('after-hours', 'standby')),
    CONSTRAINT tips_status CHECK (status IN ('pending', 'deferred', 'applied', 'verified', 'rejected')),
    CONSTRAINT tips_saving CHECK (saving_kwh_month >= 0)
);

CREATE UNIQUE INDEX tips_device_kind_key ON tips (device_id, kind);
CREATE INDEX tips_business_status_idx ON tips (business_id, status);

-- when the tips of the business were last worked out, so a read does not repeat the work more than every few minutes
ALTER TABLE businesses ADD COLUMN tips_refreshed_at timestamptz;
