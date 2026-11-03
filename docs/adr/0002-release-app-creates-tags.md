# 0002 Only the release GitHub App creates version tags

Status: Accepted, 2026-11-01

## Context

A tag that matches `v*.*.*` starts the Docker workflow and publishes an image, and an immutable release cannot be edited afterwards. Anyone with write access can push a tag, so one wrong or unreviewed tag would publish an image that nobody can take back. A tag or a pull request that a workflow makes with its default token does not start other workflows, so Release Please needs a token of another kind.

## Decision

The release workflow gets its token from the GitHub App `syafa-bot`. Two tag rulesets protect `v*`. `release-tags` forbids deleting, moving and updating a tag for everyone. `release-tags-creation` forbids creating one, and the App is its only bypass actor. Releases are immutable. The App key is the secret `RELEASE_APP_PRIVATE_KEY` of the `release` environment, which accepts only the `main` branch, and the Docker Hub token is a secret of the `docker-hub` environment, which accepts `main` and the version tags.

## Consequences

- Nobody creates a version tag by hand, including the owner. A release only happens when the release pull request is merged.
- A wrong release is never edited. The next patch version supersedes it, and the wrong version stays published under its own tag.
- If the App breaks, releases stop until it is fixed. The owner can edit the ruleset in the repository settings, which is the deliberate way around the rule and not a bypass that is open all the time.
- The App makes the release commits through the API, and GitHub signs those, so they pass the rule for verified signatures.
- The rule depends on `main`. The ruleset of `main` needs a pull request but no approval ([0003](0003-required-ci-gates.md)), so a person with write access can merge a change to a workflow. The checks on the pull request and the environments that guard the secrets are the defense against that.

The steps are in [CI/CD](../ci-cd.md#releases).
