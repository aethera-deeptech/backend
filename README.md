# Backend

Backend of an energy audit dashboard for small businesses (UMKM) in Indonesia, built for the UNS Innovation Hackathon 2026. The dashboard is in the [frontend repository](https://github.com/aethera-deeptech/frontend) and its interface is in Indonesian.

## Status

This repository holds the repository baseline and the first piece of the service, a Go program with a health route, a readiness route, a loader for its settings and a PostgreSQL layer with embedded migrations. It has no API routes yet. The routes that the frontend reads are listed in `docs/api-assumptions.md` of the frontend repository. This repository will publish its own API description when the routes exist.

## Plan

These choices are made and not built yet.

- A service written in Go with a PostgreSQL database
- A JSON REST API under `/v1` that the dashboard calls with a session cookie
- One business for each account, with a tenant id on every row so that more people can join a business later
- Readings from the devices of a business, which reach the service through one hub over HTTPS
- Tips, forecasts and monthly reports that the service calculates from those readings
- A Docker image published signed to GitHub Container Registry and mirrored to Docker Hub
- Release Please for the version and the changelog

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you open a pull request. The checks and the rules of the `main` branch are described in [docs/ci-cd.md](docs/ci-cd.md). Report security problems privately as described in [SECURITY.md](SECURITY.md). Everyone who takes part follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

This project is licensed under the GNU General Public License version 3. See [LICENSE](LICENSE).
