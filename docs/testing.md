# Testing

This page explains how the service is tested, which tests run where, and how to run them on a machine.

## What runs on every pull request

The `Tests` job of [ci.yaml](../.github/workflows/ci.yaml) builds the program and runs every test with the race detector, in a random order and without the test cache. It measures the statements of `internal/` and sends the result to Codecov. The `Lint` job checks the format, that `go.mod` is tidy, `go vet` and the linters of `.golangci.yml`. The list of every workflow and every required check is in [CI/CD](ci-cd.md).

| Kind of test | Where | What it checks |
|:---|:---|:---|
| Unit tests | next to the code, `*_test.go` | Pure rules such as the status of a device, the scrub of an error report and the period of a report |
| Route tests | `internal/<package>`, with `internal/testutil` | The routes of a package against a real PostgreSQL, through the same Fiber app that production runs |
| Fuzz targets | `fuzz_test.go` in four packages | The scrub of an error report, the check of a name, the check of a date and the checks of a batch of a gateway |
| Benchmarks | `bench_test.go` | The scrub of an error text, the PDF of a report, the reading of a batch of a gateway and the tips refresh for one device |

## How the database tests work

A route test needs PostgreSQL. `internal/database/dbtest` gives each test its own empty database with every migration applied, and drops it when the test ends. The server comes from the variables `TEST_DB_HOST`, `TEST_DB_PORT`, `TEST_DB_USER` and `TEST_DB_PASSWORD`. Without `TEST_DB_HOST` a test on a machine skips itself, so a plain `go test` still passes. A run with the variable `CI` set fails instead, so CI can never pass with every database test skipped.

`internal/testutil` builds the app in process, signs up an owner, seeds a gateway, a plug and a session, and sends requests through the app with `app.Test`. It starts no network port, and the tests send no real mail. The tests replace the mailer with an outbox that keeps the verification and reset links and counts the account deleted mails, and a test reads the link or the count from it when it needs one.

To run them on a machine, point the variables at a throwaway PostgreSQL. A killed run leaves its database behind, so never use a server that holds real data.

```sh
docker run --rm -d --name pg-test -e POSTGRES_PASSWORD=postgres -p 5432:5432 postgres:18-alpine
until docker exec pg-test pg_isready -U postgres; do sleep 1; done
export TEST_DB_HOST=127.0.0.1 TEST_DB_USER=postgres TEST_DB_PASSWORD=postgres
go test ./... -race -count=1
```

A smaller run names one package, for example `go test ./internal/relay -count=1`, and uses the same variables. Stop the server when you are done.

```sh
docker stop pg-test
```

## The checks of a pull request with a database

These workflows run against a throwaway PostgreSQL. Most of them build the image of the pull request and start the whole stack as the server runs it. `explain.yaml` runs a Go test against PostgreSQL only, and `migrations.yaml` lints the migration files and starts nothing.

| Workflow | What it checks | Required |
|:---|:---|:---|
| `smoke-stack.yaml` | Signs a business up, claims a gateway the way the firmware does, pairs a plug, sends a reading and reads every screen of the app | yes |
| `perf.yaml` | A business with 6 gateways and 24 plugs under a k6 script, with limits on the time of a request | yes |
| `explain.yaml` | The query plans of the main read routes with 100 devices and 400 days of data | yes |
| `dast.yaml` | The ZAP baseline scan on a seeded stack. It is not a required check | no |
| `migrations.yaml` | squawk lints the migrations that a pull request adds or changes, for statements that lock a table. A pull request that adds none ends the job at once with a green result | yes |

## The checks that run on a schedule

| Workflow | When | What it does |
|:---|:---|:---|
| `fuzz.yaml` | Saturday | Runs each fuzz target for 3 minutes and keeps the input that made one fail |
| `mutation.yaml` | Sunday | Changes the code of four packages a little at a time and runs their tests, so a change that no test notices shows a line that is run but not checked |
| `dast-active.yaml` | Sunday | The ZAP active scan on a seeded stack. It only reports |
| `bench.yaml` | Wednesday | Compares the benchmarks of `main` with those of a week before. It only reports |
| `perf.yaml` | daily | The longer k6 script |
| `dast.yaml` | Monday | The same ZAP baseline scan as on a pull request |
| `links.yaml` | weekly | Checks the links of the Markdown files |

## The smoke test of production

`smoke.yaml` is started by hand after a deploy. It signs in to the deployed API as the keeper account, opens every screen of the app once, and checks that `/healthz` reports the version that was deployed. It sends no request that changes data that an owner entered. It is the last step of a deploy.

## Coverage

Codecov holds the coverage of `internal/` and the patch coverage of each pull request. Neither is a required check, because the status can arrive after the other checks. A change that adds code with no test shows up in the comment that Codecov posts on the pull request.
