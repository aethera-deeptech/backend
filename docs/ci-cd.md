# CI/CD

## Workflows

| Workflow | Trigger | What it does |
| :--- | :--- | :--- |
| `commits.yaml` | pull requests | Lints the commits of the pull request with commitlint and its Conventional Commits rules, which are set in `commitlint.config.mjs`, and checks the pull request title against the same types. It never cancels a run, runs the whole job on every edit, including an edit of the body only, and reads the title from the pull request when it runs, so editing the body of a pull request cannot leave a cancelled required check behind and a rerun checks the current title. Merge commits and the messages that commitlint ignores by default, such as the ones that start with `Revert`, `fixup!` or `squash!`, are skipped, and the line lengths of the body and footer are not limited because Renovate writes long ones |
| `prformat.yaml` | pull requests | Checks that the branch is named `type/short-description` with a type of Conventional Commits, that the pull request has a label and an assignee, and that the body is only hyphen bullets with text. A pull request that a bot opened (Renovate, Dependabot or Release Please) from its own branch is skipped. It never cancels a run and reads the pull request when it runs, so a label or an assignee that was added later is picked up by a rerun of the failed run or by an edit of the body. Adding a label or an assignee starts no run, and the newest run of the check is the one that counts |

### Jobs

| Workflow | Job | What it checks |
| :--- | :--- | :--- |
| `commits.yaml` | Conventional commits | The commit messages with commitlint and the pull request title with a pattern for the same types |
| `prformat.yaml` | PR format | The branch name, the label, the assignee and the body of the pull request |

More workflows arrive with the pull requests that need them. The jobs for the Go code, the Docker image, the security scans and the releases are added together with the code they check.

## Required checks

The ruleset of `main` requires a pull request that is merged with a merge commit, resolved review threads, branches that are up to date with `main` (strict checks), and it refuses deletion and force pushes. A new push dismisses stale approvals. The ruleset has no bypass actors.

Signed commits are not required yet. The rule is added after the release App has made its first verified commit, and this page changes in the same pull request that turns it on.

Every check below is required and is reported by GitHub Actions.

| Check | Reported by | Required |
| :--- | :--- | :--- |
| Conventional commits | GitHub Actions | yes |
| PR format | GitHub Actions | yes |

Each required check is pinned to the app that reports it, so another app cannot post a check with the same name. When a pull request adds, renames or removes a job, the ruleset is updated before the pull request is merged.

The tags that start with `v` are protected by two more rulesets. `release-tags` refuses deleting, moving and updating them. `release-tags-creation` lets only the release App create them.
