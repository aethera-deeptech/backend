# Governance

The backend is a small project with one maintainer. This page says how changes are accepted so that nobody has to guess. The maintainer is listed in [MAINTAINERS.md](MAINTAINERS.md).

## Decisions

The maintainer makes the final call on what is merged, what is released and which direction the project takes. Most decisions happen in a pull request or an issue, in the open, so the reasoning stays on record.

The API is shared with the dashboard in the [frontend repository](https://github.com/aethera-deeptech/frontend), and the gateways call it too, so a change to the contract needs more care. That covers JSON field names, enum values, error codes and status codes. Such a change is described in the pull request and written into `openapi.yaml` in the same pull request. A change that breaks existing clients is marked as breaking in the commit header and called out in the pull request body.

## Changes

Every change reaches `main` through a pull request. The ruleset on `main` requires the following for everyone, including the maintainer.

- The required checks pass and the branch is up to date with `main`.
- Review threads are resolved.
- Commits are signed and follow Conventional Commits.
- The merge is a merge commit.

The ruleset does not require an approving review. Review from another person is welcome on any pull request and expected on a change to the API contract, but it is a habit and not a rule that GitHub enforces. [CONTRIBUTING.md](CONTRIBUTING.md) has the practical steps and [CI/CD](docs/ci-cd.md) lists the checks.

## Releases

Releases are automated. Release Please opens a release pull request from the commit messages that reach `main`, and the maintainer merges it when the release is ready. Merging creates the tag and the GitHub release through the release GitHub App, which is the only account allowed to create a `v*` tag. Nobody can delete, move or update a release tag. The release App key is a secret of the `release` environment, which accepts only the `main` branch. The Docker Hub token is a secret of the `docker-hub` environment, which accepts only the `main` branch and `v*.*.*` tags. A workflow on any other branch cannot read either secret. A merge only publishes the image. The deployment to the server is a separate step that the maintainer runs.

## Becoming a collaborator

Anyone can contribute with a fork and a pull request. The maintainer decides who gets write access and invites that person, and the maintainer can remove the access again.

## Conduct and security

Everyone follows the [Code of Conduct](CODE_OF_CONDUCT.md). Vulnerabilities are reported privately as described in [SECURITY.md](SECURITY.md).

## Changing this page

Changes to this page, to [MAINTAINERS.md](MAINTAINERS.md) and to the rulesets go through a pull request like any other change, and the maintainer decides.
