-- A room is a place in the business where a device sits, for example the sewing room. The name is unique inside a business, whatever the case.
CREATE TABLE rooms (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id uuid NOT NULL REFERENCES businesses (id) ON DELETE CASCADE,
    name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT rooms_name_length CHECK (char_length(name) BETWEEN 1 AND 40)
);

CREATE UNIQUE INDEX rooms_business_name_key ON rooms (business_id, lower(name));
