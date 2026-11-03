# Roadmap

This is the roadmap of the backend. It shows what the API offers today and what is planned. A roadmap is a plan and not a promise. Items move, and the maintainer decides the order.

## Done

- Accounts for one business each, with registration, email verification, login with a session cookie, password reset and change of email. The data of a business hangs off one business row, so more people can join a business later.
- Gateways that enroll themselves, are claimed by the owner and send readings in numbered batches. The [firmware guide](docs/firmware-guide.md) explains the protocol.
- Pairing of plugs and clamps on a gateway, rooms, and the status of every device.
- Tips that the service works out from the readings, with states from pending to verified and a saving that is checked against the energy per piece.
- Anomaly detection for a surge, an overload and a motor that shows signs of wear.
- The home screen, the production records and the monthly and weekly reports with a PDF.
- Relay control, with an owner switch and a schedule that cuts power at a set time. A command rides the answer to the next batch of the gateway.
- Health and readiness routes, Prometheus metrics, error reports and the files for Grafana Cloud with Alloy, see [Monitoring](docs/monitoring.md).
- CI with signed commits, signed images, security scans, a performance test and a ZAP scan, see [CI/CD](docs/ci-cd.md).

## Planned, not started

- Sign in with Google. The contract in `openapi.yaml` already describes the route, but the service does not serve it yet.
- A log archive in an object store, which keeps the logs longer than the host does.
- More documentation pages, such as the data model and the architecture.
