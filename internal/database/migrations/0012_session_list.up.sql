-- The owner can see the sessions of the account and end one of them. A session gets an id that the app can name, and a short label of the
-- browser and the system that made it. Both columns have a default, so the version before this one, which inserts a session with only the
-- hash and the user, keeps working in a rolling update, and the sessions that exist now get the label Unknown device.
SET LOCAL lock_timeout = '10s';

-- The default is volatile, so the table is written again once. The table holds one row for each open session, at most 20 for an account, and
-- the lock timeout above stops the migration when a long query holds the table. No unique index is made, because a lookup always names the
-- user too, and the index of the user finds the few rows of the account.
-- squawk-ignore adding-field-with-default
ALTER TABLE sessions ADD COLUMN id uuid NOT NULL DEFAULT gen_random_uuid();

ALTER TABLE sessions ADD COLUMN label text NOT NULL DEFAULT 'Unknown device';

-- The table is small and the old rows all have the default, so the check of them is short.
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE sessions ADD CONSTRAINT sessions_label_length CHECK (char_length(label) BETWEEN 1 AND 60);
