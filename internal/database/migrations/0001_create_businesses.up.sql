-- A business is a tenant. Every other table of the service points to one business, so a query can never read the rows of another one.
CREATE TABLE businesses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- null until the owner names the business, which happens after a sign-up with Google
    name text,
    -- an IANA zone name. Readings are stored in UTC and this zone decides the day, the month and the work hours.
    time_zone text NOT NULL DEFAULT 'Asia/Jakarta',
    -- null means the default tariff of the service
    tariff_rp_per_kwh integer,
    work_days text NOT NULL DEFAULT 'mon-sat',
    work_start time NOT NULL DEFAULT '08:00',
    work_end time NOT NULL DEFAULT '17:00',
    anomaly_sensitivity text NOT NULL DEFAULT 'sedang',
    production_unit text NOT NULL DEFAULT 'potong',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT businesses_name_length CHECK (name IS NULL OR char_length(name) BETWEEN 1 AND 80),
    CONSTRAINT businesses_tariff_range CHECK (tariff_rp_per_kwh IS NULL OR tariff_rp_per_kwh BETWEEN 1 AND 100000),
    CONSTRAINT businesses_work_days CHECK (work_days IN ('mon-fri', 'mon-sat', 'daily')),
    CONSTRAINT businesses_work_hours CHECK (work_end > work_start),
    CONSTRAINT businesses_sensitivity CHECK (anomaly_sensitivity IN ('rendah', 'sedang', 'tinggi')),
    CONSTRAINT businesses_production_unit CHECK (char_length(production_unit) BETWEEN 1 AND 20)
);
