# 0001 Pull requests are merged with merge commits

Status: Accepted, 2026-11-01

## Context

Release Please reads the Conventional Commit messages that reach `main` to decide the next version and to write the changelog. A squash merge would replace the commits of a pull request with one commit, and a rebase merge would rewrite them. The owner also wants the individual signed commits of each pull request to stay on `main`, so that a change can be traced to the step that made it.

## Decision

The ruleset of `main` allows only the merge method `merge`, and the repository settings turn squash and rebase merges off. GitHub writes the merge commit with its default subject and the title of the pull request as the body.

## Consequences

- Every commit of a pull request is on `main`, so every commit message has to follow Conventional Commits. The check "Conventional commits" tests each commit and the title of the pull request.
- The history is not linear. A reader follows the merge commits to see one pull request at a time.
- The merge commit of a pull request is made and signed by GitHub, so it passes the rule that every commit on `main` has a verified signature.
- A change to the branch of a pull request that only merges `main` into it has the message `Merge branch 'main' into <branch>`, which the commit check accepts.

The rules of the branch are in [CI/CD](../ci-cd.md#required-checks).
