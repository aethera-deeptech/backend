# Data model

The data is in PostgreSQL, and the schema comes from the migrations in `internal/database/migrations`. They are numbered, embedded in the binary and applied in order when the program starts. This page lists the tables and the rules that matter, and the files are the source of truth.

## The business is the tenant

A business is the owner of its data. An account belongs to one business through `users.business_id`, so more people can join a business later. The rows of a business hang off its row, directly through `business_id` or through a gateway. Deleting a business deletes everything under it, because the keys use `ON DELETE CASCADE`.

## Accounts and sessions

| Table | What it holds | Rules |
|:---|:---|:---|
| `businesses` | Name, time zone, tariff, work days and hours, anomaly sensitivity, production unit | The name is 1 to 80 characters, the tariff is 1 to 100000 rupiah for each kWh, the work days are `mon-fri`, `mon-sat` or `daily`, and the work end is after the work start |
| `users` | Email, password hash, Google subject, verified time, consent time and version | The email is lower case and unique. An account needs a password hash or a Google subject |
| `sessions` | One row for each session, by the SHA-256 hash of the token | Deleted by the janitor when it is past its lifetime |
| `email_tokens` | Verification and reset tokens, by hash | The purpose is `verify` or `reset`, and a token has an expiry and a used time |
| `rate_limit_events` | One row for each counted request | An unlogged table, so a crash of the database may empty it, and the limits start over |

## Devices

| Table | What it holds | Rules |
|:---|:---|:---|
| `gateways` | Name, hardware id, key hash, last sequence number, last seen time | The hardware id and the key hash are unique, and the key hash is 32 bytes |
| `gateway_enrollments` | The claim code and the poll id, both by hash, and the key hash | A row expires, and it points to the gateway after the owner claims it |
| `nodes` | A plug or a clamp that a gateway reported, keyed by gateway and node id | The kind is `plug` or `clamp`. It keeps the last sample time, the last energy counter and the relay state |
| `devices` | A node that the owner paired, with a name and a room | One device for each node, and the room is required |
| `pairings` | A search for a new device | The status is `searching`, `connected`, `failed` or `cancelled`, and a business has one search at a time |
| `rooms` | The rooms of a business | The name is 1 to 40 characters and unique for a business without regard to case |

## Readings and anomalies

| Table | What it holds | Rules |
|:---|:---|:---|
| `readings` | The raw samples, keyed by gateway, node and time | Power from 0 to 60000 W, and optional volts, amps, energy, frequency, power factor and node state. The janitor keeps 30 days |
| `readings_hourly` | The sums of an hour, keyed by gateway, node and UTC hour | The sample count, the sum, the largest and the smallest power, and the energy. The janitor keeps 400 days |
| `anomalies` | A surge, an overload or a degradation of a node | The kind is one of the three, and a degradation also stores the power factor |

The tips, the home screen and the reports read the hourly sums, so they stay cheap on a long history. The raw samples are only for the recent past.

## Tips, production and relay

| Table | What it holds | Rules |
|:---|:---|:---|
| `tips` | One tip for each device and kind, with its saving and its decision times | The kind is `after-hours` or `standby`, and the status is `pending`, `deferred`, `applied`, `verified` or `rejected` |
| `production_records` | The pieces made on a day, keyed by business and day | The quantity is 1 to 100000000 |
| `relay_commands` | A command to switch a relay | The action is `on` or `off`, the source is `owner` or `schedule`, and a device has one open command at a time. The janitor keeps 30 days |
| `relay_schedules` | One schedule for each device with an off time, an optional on time and the days | The on time differs from the off time |

## Changing the schema

A migration is a pair of `up` and `down` files. Squawk lints every migration that a pull request adds, so a statement that is known to lock a table is refused. A migration that alters a table sets a lock timeout first, as 0009 and 0010 do. The migrations run in order, so two pull requests that add a migration must merge in the order of their numbers. A destructive migration needs a backup first, see [Operations](operations.md).
