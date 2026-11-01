-- One record for each business and each day. The day is the calendar day in the time zone of the business, so it is a date and not a
-- moment. The unit is kept with the record, so a business that changes its unit later keeps what it counted before.
CREATE TABLE production_records (
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    day date NOT NULL,
    quantity integer NOT NULL,
    unit text NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (business_id, day),
    CONSTRAINT production_quantity_range CHECK (quantity BETWEEN 1 AND 100000000),
    CONSTRAINT production_unit_length CHECK (char_length(unit) BETWEEN 1 AND 20)
);
