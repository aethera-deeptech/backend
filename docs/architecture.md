# Architecture

The backend is one Go program that serves a JSON API for the dashboard under `/v1` and a device API for the gateways under `/device/v1`. It keeps all of its data in PostgreSQL. The contract is [openapi.yaml](../openapi.yaml), and [API](api.md) explains the decisions behind it.

## Parts

| Part | What it does |
|:---|:---|
| API server | A Fiber v3 HTTP server that handles routing, security headers, rate limits and errors |
| PostgreSQL | The only store. It holds accounts, sessions, readings, tips and every rate limit counter, and the program talks to it through pgx |
| Migrations | SQL files that are embedded in the binary and applied when the program starts, see [Data model](data-model.md) |
| Mail | Sends the verification and reset mails over SMTP. A request never waits for the mail server |
| Metrics listener | An optional second listener that serves the Prometheus text format, see [Monitoring](monitoring.md) |
| Error reports | Optional reports of a 5xx and a panic to Sentry, with a scrubbed text |

There is no Redis and no object storage. Because every copy of the program keeps its state in the database, two copies behind a reverse proxy behave like one, and a deploy replaces them one at a time, see [Operations](operations.md).

## Packages

The code is in `cmd/api` and in the packages under `internal`. A feature package has its HTTP handlers, its rules and its SQL together.

| Package | Purpose |
|:---|:---|
| `cmd/api` | The program. It reads the settings, opens the database, starts the background tasks and serves |
| `internal/server` | Builds the HTTP application, the health routes and the middleware order |
| `internal/app` | Wires the routes of every feature, and the tests build the same application |
| `internal/api` | What every route shares, which is the error type, the strict JSON reader and the middleware |
| `internal/auth` | Sign up, email verification, sign in, sessions, password reset and change of email |
| `internal/business` | The settings of a business and its rooms |
| `internal/gateway` | Enrollment and claim of gateways, the ingest of readings, the hourly sums and the anomaly checks |
| `internal/device` | The devices that an owner paired, their status and their detail |
| `internal/tips` | Works out the tips from the readings and keeps the decisions of the owner |
| `internal/home` | Builds the home screen from the devices, the tips and the energy |
| `internal/production` | The daily production records, which give the energy for each piece |
| `internal/report` | The monthly and weekly report, as data and as one page of PDF |
| `internal/relay` | Switching the relay of a plug, with commands and schedules |
| `internal/janitor` | The clean up work that runs on a timer |
| `internal/config` | Loads the settings from environment variables |
| `internal/database` | Opens the pool and applies the migrations |
| `internal/mail`, `internal/errorreport`, `internal/metrics`, `internal/logging`, `internal/healthcheck` | Mail, error reports, metrics, JSON logs and the probe that the container health check runs |
| `internal/testutil`, `internal/explain` | Test helpers, and a tool that checks the query plans of the read routes on a seeded database |

## The path of a request

1. The server recovers from a panic, sets the security headers and writes one access log line for each request.
2. A route under `/v1` also goes through the CORS and the CSRF checks. A request that changes data must carry the header `X-Requested-With`, and its `Origin` must be the origin of the app.
3. The session middleware finds the account from the cookie. A request of a gateway has no session, and it carries the key of the gateway as a bearer token instead.
4. The handler reads the body with the strict reader, which refuses an unknown field, and calls the rules of its feature.
5. The rules run SQL, and the error handler turns any failure into the JSON form of [Error codes](errors.md).

## The path of a reading

A gateway sends a numbered batch of readings. The service checks the key and the sequence number, then writes the samples to the raw table and adds them to the hourly sums in the same transaction. It checks the samples for an anomaly, which is best effort and never loses the readings, and answers with the relay commands that wait for the gateway. The [firmware guide](firmware-guide.md) describes the protocol, and the tips, the home screen and the reports read the hourly sums.

## Background tasks

Every copy of the program starts two timers. Only one copy does a round at a time, because a round takes an advisory lock of PostgreSQL first.

| Task | Every | What it does |
|:---|:---|:---|
| Janitor | hour | Deletes sessions, mail tokens and rate limit events that are past their time, unverified businesses, raw readings older than 30 days, hourly sums older than 400 days, ended pairings and enrollments, and relay commands older than 30 days |
| Relay scheduler | minute | Makes the commands of the schedules that have a time that passed |

## Security in short

The service stores no password and no session token in clear text. A password is hashed, and a session, a mail token and a gateway key are stored as a SHA-256 hash. Every query of the dashboard is limited to the business of the signed in account, and an id of another business answers like an id that does not exist.
