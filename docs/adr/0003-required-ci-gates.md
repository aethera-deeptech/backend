# 0003 Which checks gate a merge into main, and why no approval is required

Status: Accepted, 2026-11-01

## Context

The repository has one owner and no second reviewer. A pull request that needs an approving review could never be merged by its author, because GitHub does not let an author approve their own pull request. A required review would also add a person who does not exist today.

## Decision

The ruleset of `main` has no bypass actors. It requires a pull request that is merged with a merge commit ([0001](0001-merge-commits-only.md)), resolved review threads, verified signatures on every commit, branches that are up to date with `main`, and a set of status checks. It requires no approving review and no code owner review, and it refuses deletion and force pushes. A `CODEOWNERS` file asks the owner for a review, but it never blocks a merge.

Every required check is pinned to the app that reports it, so another app cannot post a check with the same name. A check that can be missing on a pull request of a bot or of a fork, that arrives late, or that runs only on a schedule is not required. That covers the ZAP active scan, fuzzing, mutation tests, Scorecard, the Codecov status and the CodeRabbit check. The list of required checks is in [CI/CD](../ci-cd.md#required-checks).

## Consequences

- The checks and the review of the diff by the owner are the only gate between a change and production. A review agent reads each pull request before it is merged, and it shares the blind spots of the model that wrote the change.
- A person with write access can merge a pull request that changes a workflow. The checks of that pull request run first, but a workflow runs with the permissions of its own file, so the rules for pinned actions, least privilege and the environments of secrets matter more than they would with a second reviewer.
- OSSF Scorecard reports the missing approvals as the alerts Code-Review and Branch-Protection. They describe a repository with one owner and stay open. They are not dismissed.
- A new job or a renamed job changes the ruleset in the same pull request, or the new check would not gate anything.
- When a second maintainer joins, requiring one approving review is the first change to make, and this record is then replaced.
