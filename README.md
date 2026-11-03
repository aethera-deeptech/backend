# Backend

[![CI](https://github.com/aethera-deeptech/backend/actions/workflows/ci.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/ci.yaml)
[![Security](https://github.com/aethera-deeptech/backend/actions/workflows/security.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/security.yaml)
[![Docker](https://github.com/aethera-deeptech/backend/actions/workflows/docker.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/docker.yaml)
[![Docker Hub mirror](https://github.com/aethera-deeptech/backend/actions/workflows/docker-hub-mirror.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/docker-hub-mirror.yaml)
[![Config lint](https://github.com/aethera-deeptech/backend/actions/workflows/config.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/config.yaml)
[![Performance](https://github.com/aethera-deeptech/backend/actions/workflows/perf.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/perf.yaml)
[![DAST](https://github.com/aethera-deeptech/backend/actions/workflows/dast.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/dast.yaml)
[![DAST active](https://github.com/aethera-deeptech/backend/actions/workflows/dast-active.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/dast-active.yaml)
[![Vale](https://github.com/aethera-deeptech/backend/actions/workflows/vale.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/vale.yaml)
[![Fuzz](https://github.com/aethera-deeptech/backend/actions/workflows/fuzz.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/fuzz.yaml)
[![Mutation](https://github.com/aethera-deeptech/backend/actions/workflows/mutation.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/mutation.yaml)
[![Links](https://github.com/aethera-deeptech/backend/actions/workflows/links.yaml/badge.svg?branch=main)](https://github.com/aethera-deeptech/backend/actions/workflows/links.yaml)

[![Release](https://img.shields.io/github/v/release/aethera-deeptech/backend?sort=semver)](https://github.com/aethera-deeptech/backend/releases)
[![License](https://img.shields.io/github/license/aethera-deeptech/backend)](LICENSE)
[![Go version](https://img.shields.io/github/go-mod/go-version/aethera-deeptech/backend)](go.mod)

[![API](https://img.shields.io/website?url=https%3A%2F%2Faethera-api.syafahadyan.com%2Fhealthz&label=api)](https://aethera-api.syafahadyan.com/healthz)
[![Docker pulls](https://img.shields.io/docker/pulls/syafa/aethera-backend)](https://hub.docker.com/r/syafa/aethera-backend)
[![Docker image size](https://img.shields.io/docker/image-size/syafa/aethera-backend/latest)](https://hub.docker.com/r/syafa/aethera-backend)
[![GHCR](https://img.shields.io/badge/ghcr.io-aethera--deeptech%2Fbackend-2088FF?logo=github)](https://github.com/aethera-deeptech/backend/pkgs/container/backend)

[![Last commit](https://img.shields.io/github/last-commit/aethera-deeptech/backend)](https://github.com/aethera-deeptech/backend/commits/main)
[![Conventional Commits](https://img.shields.io/badge/conventional%20commits-1.0.0-FE5196)](https://www.conventionalcommits.org)
[![Renovate](https://img.shields.io/badge/renovate-enabled-brightgreen?logo=renovatebot)](https://docs.renovatebot.com/)

[![codecov](https://codecov.io/gh/aethera-deeptech/backend/graph/badge.svg?token=EA3QTLF30E)](https://codecov.io/gh/aethera-deeptech/backend)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/aethera-deeptech/backend/badge)](https://scorecard.dev/viewer/?uri=github.com/aethera-deeptech/backend)

Backend of an energy audit dashboard for small businesses (UMKM) in Indonesia, built for the UNS Innovation Hackathon 2026. The dashboard is in the [frontend repository](https://github.com/aethera-deeptech/frontend) and its interface is in Indonesian.

## Status

The service is built and runs in production. It is a Go program with a PostgreSQL database that serves a JSON API under `/v1` for the dashboard and a device API under `/device/v1` for the gateways. The contract is [openapi.yaml](openapi.yaml), and [docs/api.md](docs/api.md) explains the decisions behind it. [ROADMAP.md](ROADMAP.md) lists what is done and what is planned.

## Stack

- Go with Fiber v3 and pgx
- PostgreSQL, with SQL migrations that are embedded in the binary and applied when the service starts
- Settings from environment variables, read once at start
- Reports as one page of PDF, made in the service
- Errors to Sentry and metrics to Prometheus or Grafana Cloud, both optional

## Quick start

The service needs a PostgreSQL that it can reach. It does not read a `.env` file, so set the variables in your shell. `DB_HOST`, `DB_NAME`, `DB_USERNAME` and `DB_PASSWORD` are required, and the mail settings are only required in production, because a development run logs the mails instead of sending them.

```sh
export DB_HOST=localhost DB_NAME=aethera DB_USERNAME=aethera DB_PASSWORD=change-me DB_SSL_MODE=disable
go run ./cmd/api
```

`GET /healthz` reports the state of the database and the mail setting, and answers 503 when the database is down. `GET /readyz` is the check that a copy is ready for traffic. [Operations](docs/operations.md) explains the production setup with two copies behind a reverse proxy.

## Configuration

Every setting is an environment variable, and [.env.example](.env.example) lists all of them with their comments. The ones that matter most are these.

| Variable | Default | What it does |
|:---|:---|:---|
| `APP_ENV` | `development` | `production` turns on the stricter checks, such as an https `APP_ORIGIN` and a mail server and sender |
| `APP_PORT` | `8080` | The port of the API |
| `APP_ORIGIN` | `http://localhost:5173` | The one origin that CORS allows, and the base of the links in the emails |
| `TRUST_PROXY` | `false` | Read the client address from `PROXY_HEADER` when a reverse proxy sits in front. The header is accepted only from private and loopback addresses, so also set `TRUST_PROXY_CIDRS` to the address of the proxy, or another container on a shared network can set the client address of the rate limits |
| `DB_HOST`, `DB_NAME`, `DB_USERNAME`, `DB_PASSWORD` | required | The PostgreSQL connection. `DB_PORT` is 5432 unless it is set |
| `DB_SSL_MODE` | `require`, and `disable` in the example | The mode of the connection. Use `disable` only on a private Docker network or on your own machine |
| `DB_MAX_CONNS` | `8` | The pool size of one copy |
| `SMTP_HOST`, `SMTP_PORT`, `SMTP_SECURITY`, `SMTP_FROM_ADDRESS` | see the file | The mail server for the verification and reset mails |
| `SENTRY_DSN` | empty | An empty value switches error reports off |
| `METRICS_ADDRESS` | empty | The address of the Prometheus listener. Empty switches it off |

The settings of Alloy live in `grafana/alloy/.env`, see [Monitoring](docs/monitoring.md).

## Documentation

| Page | What it answers |
|:---|:---|
| [API](docs/api.md) | The decisions behind the API, such as the sessions, the status of a device, the anomalies, the tips and the relay |
| [openapi.yaml](openapi.yaml) | The full contract of every route |
| [Errors](docs/errors.md) | Every error code, its status and what the client should do |
| [Firmware guide](docs/firmware-guide.md) | The protocol of the gateway, with the limits and the relay commands |
| [Architecture](docs/architecture.md) | How the packages fit together and which jobs run |
| [Data model](docs/data-model.md) | The tables and how long the data stays |
| [Security](docs/security.md) | The threats, the measures and the known limits |
| [Privacy](docs/privacy.md) | What is kept, for how long, who can read it and how it is deleted |
| [Operations](docs/operations.md) | How to run and update the service on a server |
| [Monitoring](docs/monitoring.md) | The Grafana files, the alerts and the dashboards |
| [CI/CD](docs/ci-cd.md) | Every workflow and every required check |
| [ROADMAP.md](ROADMAP.md) | What is done and what is planned |

## Project layout

```text
cmd/api            The entry point of the service
internal/          The packages of the service, one folder for each part
  api              Errors, the shared middleware and the access log
  app              Wires the packages to the routes
  auth             Accounts, sessions, password reset and the rate limits
  business         The settings of a business
  device           Pairing, rooms and the views of a device
  gateway          Enrollment, the intake of readings, the rollups and the anomaly rules
  relay            The commands and the schedules of a plug
  tips             The tips and the check of a saving
  home, report     The home screen and the weekly and monthly reports
  production       The production records of a business
  config           Settings from the environment
  database         The connection and the migrations
  server, mail, logging, metrics, errorreport, healthcheck, janitor   Support for running the service
  testutil, explain   Helpers for the tests and the query plan tool
docs/              The pages listed above
grafana/           Alloy, alert rules and dashboards
k6/, smoke/        The performance test and the smoke test
scripts/           The rolling update script
```

## How it fits together

- Each account has one business, and the data of a business hangs off one business row, so more people can join a business later.
- The dashboard calls the API with a session cookie.
- The devices of a business reach the service through one gateway over HTTPS, see the [firmware guide](docs/firmware-guide.md).
- The service works out the tips, the anomalies, the bill so far and the monthly and weekly reports from the readings.
- A Docker image is published signed to GitHub Container Registry and mirrored to Docker Hub. [Operations](docs/operations.md) explains how to run it.
- Release Please makes the version and the changelog.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you open a pull request. The checks and the rules of the `main` branch are described in [docs/ci-cd.md](docs/ci-cd.md). Report security problems privately as described in [SECURITY.md](SECURITY.md). Everyone who takes part follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

This project is licensed under the GNU General Public License version 3. See [LICENSE](LICENSE).
