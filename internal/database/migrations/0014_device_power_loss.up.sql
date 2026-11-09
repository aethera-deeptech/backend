-- The owner chooses for each device what a plug does after it loses its own power, to stay off or to go back to the state that it had.
-- The column is new and has a default, so the version before this one, which neither reads nor writes it, keeps working in a rolling
-- update. A plug stays off by default, because a machine that starts by itself after a power cut is the riskier choice.
SET LOCAL lock_timeout = '10s';

ALTER TABLE devices ADD COLUMN after_power_loss text NOT NULL DEFAULT 'off';

-- The table holds the devices of the businesses, a few thousand rows at most, and every row has the default, so the check is short.
-- squawk-ignore constraint-missing-not-valid
ALTER TABLE devices ADD CONSTRAINT devices_after_power_loss CHECK (after_power_loss IN ('off', 'last'));
