# Backend

Backend of an energy audit dashboard for small businesses (UMKM) in Indonesia, built for the UNS Innovation Hackathon 2026. The dashboard is in the [frontend repository](https://github.com/aethera-deeptech/frontend) and its interface is in Indonesian.

## Status

The service is built and runs in production. It is a Go program with a PostgreSQL database that serves a JSON API under `/v1` for the dashboard and a device API under `/device/v1` for the gateways. The contract is [openapi.yaml](openapi.yaml), and [docs/api.md](docs/api.md) explains the decisions behind it. [ROADMAP.md](ROADMAP.md) lists what is done and what is planned.

## How it fits together

- Each account has one business, and every table points to the business, so more people can join a business later.
- The dashboard calls the API with a session cookie.
- The devices of a business reach the service through one gateway over HTTPS, see the [firmware guide](docs/firmware-guide.md).
- The service works out the tips, the anomalies, the bill so far and the monthly and weekly reports from the readings.
- A Docker image is published signed to GitHub Container Registry and mirrored to Docker Hub. [Operations](docs/operations.md) explains how to run it.
- Release Please makes the version and the changelog.

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you open a pull request. The checks and the rules of the `main` branch are described in [docs/ci-cd.md](docs/ci-cd.md). Report security problems privately as described in [SECURITY.md](SECURITY.md). Everyone who takes part follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

This project is licensed under the GNU General Public License version 3. See [LICENSE](LICENSE).
