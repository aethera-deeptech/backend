# CI/CD

## Workflows

| Workflow | Trigger | What it does |
| :--- | :--- | :--- |
| `commits.yaml` | pull requests | Lints the commits of the pull request with commitlint and its Conventional Commits rules, which are set in `commitlint.config.mjs`, and checks the pull request title against the same types. It never cancels a run, runs the whole job on every edit, including an edit of the body only, and reads the title from the pull request when it runs, so editing the body of a pull request cannot leave a cancelled required check behind and a rerun checks the current title. Only the `Merge branch 'main' into <branch>` message of the Update branch button is skipped, so a `fixup!`, `squash!` or `Revert` message fails like any other, and the line lengths of the body and footer are not limited because Renovate writes long ones |
| `prformat.yaml` | pull requests | Checks that the branch is named `type/short-description` with a type of Conventional Commits, that the pull request has a label and an assignee, and that the body is only hyphen bullets with text. A pull request that a bot opened (Renovate, Dependabot or Release Please) from its own branch is skipped. It never cancels a run and reads the pull request when it runs, so a rerun checks what the pull request looks like now. Adding or removing a label or an assignee starts a new run, and the newest run of the check is the one that counts |
| `config.yaml` | push to main, pull requests, manual | Lints the configuration of the repository with the five jobs below |

### Jobs

| Workflow | Job | What it checks |
| :--- | :--- | :--- |
| `commits.yaml` | Conventional commits | The commit messages with commitlint and the pull request title with a pattern for the same types |
| `prformat.yaml` | PR format | The branch name, the label, the assignee and the body of the pull request |
| `config.yaml` | Actionlint | The syntax and the expressions of every workflow, with actionlint |
| `config.yaml` | Zizmor | The workflows for security problems, such as unpinned actions, broad permissions and injection through expressions, with zizmor |
| `config.yaml` | ShellCheck | Every tracked file that ends in `.sh`, with the settings of `.shellcheckrc` |
| `config.yaml` | File names | The names of the files and folders against `.ls-lint.yml`, with ls-lint |
| `config.yaml` | Renovate config | `renovate.json` against the schema of Renovate, in strict mode, so a renamed or removed option fails |

More workflows arrive with the pull requests that need them. The jobs for the Go code, the Docker image, the security scans and the releases are added together with the code they check.

## Required checks

The ruleset of `main` requires a pull request that is merged with a merge commit, resolved review threads, branches that are up to date with `main` (strict checks), and it refuses deletion and force pushes. A new push dismisses stale approvals. The ruleset has no bypass actors.

Signed commits are not required yet. The rule is added after the release App has made its first verified commit, and this page changes in the same pull request that turns it on.

Every check below is required and is reported by GitHub Actions.

| Check | Reported by | Required |
| :--- | :--- | :--- |
| Conventional commits | GitHub Actions | yes |
| PR format | GitHub Actions | yes |
| Actionlint | GitHub Actions | yes |
| Zizmor | GitHub Actions | yes |
| ShellCheck | GitHub Actions | yes |
| File names | GitHub Actions | yes |
| Renovate config | GitHub Actions | yes |

Each required check is pinned to the app that reports it, so another app cannot post a check with the same name. When a pull request adds, renames or removes a job, the ruleset is updated before the pull request is merged.

The tags that start with `v` are protected by two more rulesets. `release-tags` refuses deleting, moving and updating them. `release-tags-creation` lets only the release App create them.

## File names

The File names job reads the names of the files and never opens one. The rules are in `.ls-lint.yml`.

- A folder is lowercase words joined by hyphens, and a leading dot marks the folder of a tool.
- A file is lowercase words joined by hyphens, and a dot may mark a variant. ls-lint checks only the part before the first dot, so a variant part such as `BAD` in `deploy.BAD.yaml` is not checked.
- A Go file is lowercase words with no separator, and only `_test` may follow them.
- A Markdown file may also be written in capitals joined by underscores, such as `README.md` and `CODE_OF_CONDUCT.md`.
- A YAML file ends with `.yml` in every folder. A workflow ends with `.yaml` and not `.yml`, because the name of the workflow file is part of the identity that signs an image.
- The issue forms in `.github/ISSUE_TEMPLATE` are skipped, because GitHub names them by what a person sees.
- A file with no extension, such as `LICENSE`, has no extension rule.

## Dependency updates

Renovate opens the update pull requests, and `renovate.json` holds its rules. It runs on Mondays before 07:00 in Asia/Jakarta and proposes a release only after it is 7 days old. The exception is an image from `ghcr.io`, such as zizmor, because that registry gives Renovate no release time to age, so its updates are not held by the wait. It groups minor and patch updates of Go modules (`go-minor-patch`) and of Actions (`actions`), and a major update gets its own pull request. After a Go update it runs `go mod tidy`. The commit type is `chore` for Go modules and for `.node-version`, `ci` for Actions and the tools of the workflows, and `build` for the Dockerfile of the app. A routine update therefore never starts a release. The bot keeps an issue called Dependency Dashboard with every update it knows about.

Security fixes ignore the schedule and the 7 day wait. Renovate learns about them from the Dependabot alerts of the repository, so those alerts stay on. Dependabot security updates stay off, so that Renovate is the only bot that opens pull requests. A pull request that comes from such an alert gets the commit type `fix`, which does start a release.

The Renovate config job runs `renovate-config-validator --strict` on every pull request, because a broken `renovate.json` does not fail anything by itself. The bot just stops opening pull requests.

## Pinned tools

A dependency update bot cannot read a tool that is named inside a `run` step, so the tools of the jobs are listed where it can.

- `.github/tools/Dockerfile` has one stage for each image that a job starts with `docker run`, which are actionlint, zizmor and ShellCheck. Nothing builds it. The workflows get the image from `.github/tools/image.sh <stage>`, so the version of a bump is the version the jobs run.
- The versions of ls-lint and of the Renovate validator sit in the `env` of their steps. A comment such as `# renovate: datasource=npm depName=@ls-lint/ls-lint` right above the line tells Renovate where to look, so keep the comment when you edit the line.

All actions are pinned to commit SHAs with the version in a comment, and the setting "require actions to be pinned to a full-length commit SHA" is on for the repository.

## Pulling images

Docker Hub limits anonymous pulls per IP address, and every GitHub runner shares its addresses with many other users. The jobs that start a Docker Hub tool image therefore pull it through `.github/actions/pull-images`. The Zizmor image comes from ghcr.io, which has no such limit, so that job pulls it as it is.

- The action adds `mirror.gcr.io` to the registry mirrors of the Docker daemon, which serves the popular Docker Hub images without the limit.
- It then logs in to Docker Hub with a read-only token, so an image that the mirror does not have is pulled as a known account. A login that fails does not fail the job.
- `.github/actions/pull-images/pull.sh` pulls an image through the mirror first and falls back to Docker Hub.

The account name is the repository variable `DOCKERHUB_USERNAME` and the token is the secret `DOCKERHUB_PULL_TOKEN`. Without them everything still works through the mirror alone. Pull requests from forks never get the secret, so they pull through the mirror and fall back to anonymous Docker Hub pulls, which the limit can refuse.
