# Maintainers

This file lists who looks after the backend and what each person can do. [GOVERNANCE.md](GOVERNANCE.md) explains how decisions are made.

| GitHub account | Role | Access | Looks after |
| --- | --- | --- | --- |
| [@SyafaHadyan](https://github.com/SyafaHadyan) | Owner and maintainer | Admin | Merging, releases, deployment, repository settings, secrets and security reports |

## What the role means

The maintainer decides what is merged and released. Only the maintainer changes repository settings, rulesets, secrets and the production deployment, and the maintainer merges the release pull request.

A person who contributes opens pull requests like anyone else. Nobody can push to `main`, because the ruleset on `main` requires a pull request for everyone. Nobody can create a release tag by hand, because only the release GitHub App can create a `v*` tag. The release App key is a secret of the `release` environment, which accepts only the `main` branch. The Docker Hub token is a secret of the `docker-hub` environment, which accepts only the `main` branch and `v*.*.*` tags.

## Reaching a maintainer

Use an issue for bugs and ideas. Report a vulnerability through GitHub private vulnerability reporting, as described in [SECURITY.md](SECURITY.md). Reports about behavior in the community go to the maintainer the same way, and the [Code of Conduct](CODE_OF_CONDUCT.md) explains what happens next.

## Known limit

The project has one administrator. If that person is unavailable, nobody else can change the repository settings, rotate a secret or deploy. Adding a second administrator is not planned yet, and this file changes the day it happens.
