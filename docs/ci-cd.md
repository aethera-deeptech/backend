# CI/CD

## Workflows

| Workflow | Trigger | What it does |
| :--- | :--- | :--- |
| `commits.yaml` | pull requests | Lints the commits of the pull request with commitlint and its Conventional Commits rules, which are set in `commitlint.config.mjs`, and checks the pull request title against the same types. It never cancels a run, runs the whole job on every edit, including an edit of the body only, and reads the title from the pull request when it runs, so editing the body of a pull request cannot leave a cancelled required check behind and a rerun checks the current title. Only the `Merge branch 'main' into <branch>` message of the Update branch button is skipped, so a `fixup!`, `squash!` or `Revert` message fails like any other, and the line lengths of the body and footer are not limited because Renovate writes long ones |
| `prformat.yaml` | pull requests | Checks that the branch is named `type/short-description` with a type of Conventional Commits, that the pull request has a label and an assignee, and that the body is only hyphen bullets with text. A pull request that a bot opened (Renovate, Dependabot or Release Please) from its own branch is skipped. It never cancels a run and reads the pull request when it runs, so a rerun checks what the pull request looks like now. Adding or removing a label or an assignee starts a new run, and the newest run of the check is the one that counts |
| `release-please.yaml` | push to main, manual | Keeps one release pull request with the next version and `CHANGELOG.md`. Merging that pull request creates the tag and the GitHub release. It runs in the `release` environment, which only `main` can use, and it creates its token from the GitHub App `syafa-bot`. It is not a required check |
| `config.yaml` | push to main, pull requests, manual | Lints the configuration of the repository with the eight jobs below |
| `ci.yaml` | push to main, pull requests, manual | Checks and tests the Go code with the two jobs below, and uploads the coverage to Codecov |
| `security.yaml` | push to main, pull requests, daily at 20:00 UTC, manual | Runs CodeQL, the Go vulnerability check, the Go license check, dependency review, the secret scan, the Trivy filesystem scan and OSSF Scorecard. The daily run catches a CVE that is published for code that did not change. Scorecard never runs on pull requests |
| `docker.yaml` | push to main, tags, pull requests, manual | Builds the image for `linux/amd64` and `linux/arm64`, pushes it to GitHub Container Registry only, scans both platforms with Trivy, attaches an SBOM, signs it with cosign and verifies the signature right away. Only `main` writes the build cache. A pull request and a tag read it and never write one. It never logs in to Docker Hub, because a pull request can change this workflow. A pull request from a fork, a run of Dependabot and a manual run from a ref other than `main` only build the image, and the job still reports success because it had nothing to publish. A pull request from a branch of this repository, such as one of Renovate, publishes the image as `pr-<number>`. This workflow never gets a `workflow_call` trigger, because a workflow that called it would get signatures with the identity of this file |
| `docker-hub-mirror.yaml` | after `docker.yaml` finishes | Copies the image that `docker.yaml` built from GitHub Container Registry to Docker Hub by digest, as `<DOCKERHUB_USERNAME>/aethera-backend`. It runs from `main` with the `docker-hub` environment and never checks out the repository. It copies an image only when the cosign signature on it was made by `docker.yaml` at the ref and with the trigger of the run that started the mirror, and it copies that signature and verifies it again on Docker Hub before `latest` or a floating version tag moves. It is not a required check |

### Jobs

| Workflow | Job | What it checks |
| :--- | :--- | :--- |
| `commits.yaml` | Conventional commits | The commit messages with commitlint and the pull request title with a pattern for the same types |
| `prformat.yaml` | PR format | The branch name, the label, the assignee and the body of the pull request |
| `config.yaml` | Actionlint | The syntax and the expressions of every workflow, with actionlint |
| `config.yaml` | Zizmor | The workflows for security problems, such as unpinned actions, broad permissions and injection through expressions, with zizmor |
| `config.yaml` | ShellCheck | Every tracked file that ends in `.sh`, with the settings of `.shellcheckrc` |
| `config.yaml` | Hadolint | The `Dockerfile` against the rules of Hadolint, which also lints the shell commands in it |
| `config.yaml` | Dotenv lint | The names and the form of the settings in `.env.example`, with dotenv-linter. The order of the keys is not checked, because the file is grouped by topic |
| `config.yaml` | Compose config | `docker compose config` on `compose.production.example.yml` with the settings of `.env.example`. It builds and starts nothing, so it keeps the example from drifting away from the settings |
| `config.yaml` | File names | The names of the files and folders against `.ls-lint.yml`, with ls-lint |
| `config.yaml` | Renovate config | `renovate.json` against the schema of Renovate, in strict mode, so a renamed or removed option fails |
| `ci.yaml` | Lint | `gofmt`, that `go.mod` and `go.sum` are tidy, `go vet`, and golangci-lint with the linters of `.golangci.yml` |
| `ci.yaml` | Tests | `go build`, then every test with the race detector in a random order, and the coverage upload |
| `security.yaml` | CodeQL | Static analysis of the Go code, reported to code scanning |
| `security.yaml` | Go vulnerability check | `govulncheck` against the vulnerability database of Go, which fails only when the code calls the vulnerable function |
| `security.yaml` | Go licenses | The license of every package that the service and its tests import, with go-licenses. It refuses unknown licenses and AGPL, SSPL and WTFPL. The GPL family passes because the repository is under GPL-3.0 |
| `security.yaml` | Dependency review | The dependencies that a pull request adds or changes, which fails on a severity of high or above and on the license SSPL-1.0 |
| `security.yaml` | Secret scan (gitleaks) | The whole history of the repository, with gitleaks. A fixture that must stay in history goes into `.gitleaksignore` with a comment |
| `security.yaml` | Trivy filesystem scan | Vulnerabilities, secrets and misconfigurations in the files, for severity high and critical that have a fix. The ignored findings are in `.trivyignore.yml` |
| `security.yaml` | OSSF Scorecard | The security score of the repository on pushes to `main`, on the daily run and on manual runs from `main`. It never runs on pull requests or on other branches |
| `docker.yaml` | Build, scan and push image | The image build for both platforms, the Trivy scan of each platform, the SBOM, the signature and the check of that signature |

More workflows arrive with the pull requests that need them. The jobs for the releases are added together with the code they check.

## Required checks

The ruleset of `main` requires a pull request that is merged with a merge commit, resolved review threads, branches that are up to date with `main` (strict checks), and it refuses deletion and force pushes. A new push dismisses stale approvals. The ruleset has no bypass actors.

The ruleset also requires signed commits, so every commit that reaches `main` must have a verified signature. The release App signs its own commits, which was checked on the release pull request of 0.1.0 before the rule was turned on. It also has a code scanning rule that blocks a merge while CodeQL reports an alert of any severity.

Every check below is required and is reported by GitHub Actions.

| Check | Reported by | Required |
| :--- | :--- | :--- |
| Conventional commits | GitHub Actions | yes |
| PR format | GitHub Actions | yes |
| Actionlint | GitHub Actions | yes |
| Zizmor | GitHub Actions | yes |
| ShellCheck | GitHub Actions | yes |
| Hadolint | GitHub Actions | yes |
| Dotenv lint | GitHub Actions | yes |
| Compose config | GitHub Actions | yes |
| File names | GitHub Actions | yes |
| Renovate config | GitHub Actions | yes |
| Lint | GitHub Actions | yes |
| Tests | GitHub Actions | yes |
| Go vulnerability check | GitHub Actions | yes |
| Go licenses | GitHub Actions | yes |
| Dependency review | GitHub Actions | yes |
| CodeQL | GitHub Actions | yes |
| Secret scan (gitleaks) | GitHub Actions | yes |
| Trivy filesystem scan | GitHub Actions | yes |
| Build, scan and push image | GitHub Actions | yes |

Each required check is pinned to the app that reports it, so another app cannot post a check with the same name. When a pull request adds, renames or removes a job, the ruleset is updated before the pull request is merged.

The tags that start with `v` are protected by two more rulesets. `release-tags` refuses deleting, moving and updating them. `release-tags-creation` lets only the release App create them.

## Go checks

The Lint job formats, vets and lints the Go code. golangci-lint uses the linters in `.golangci.yml`. Among them, `forbidigo` keeps the program from printing to the standard output and from calling `log.Fatal` outside `cmd`, and `tagliatelle` checks that JSON names are camelCase. A comment that silences a linter has to name the linter and give a reason.

The Tests job runs every test with the race detector, in a random order and without the test cache. It measures the statements of `internal/` and uploads the result to Codecov, which reads its rules from `codecov.yml`. A failed upload fails the build on the `main` branch, and that includes a manual run on `main`. It never fails a pull request, so an outage of Codecov does not block one. The Codecov checks are not required. Every run on `main` has a concurrency group of its own, so a newer run does not cancel or replace it and every commit on `main` keeps its coverage report.

## Container image

The `Dockerfile` has two stages. The first compiles the program on the machine that builds the image, and cross-compiles for the platform of the image, so an arm64 image needs no emulation. The second is a distroless image without a shell or a package manager. It holds the one binary and runs it as the user 65532, which is not root. The base images are pinned by digest, and Renovate keeps the digests current.

The image has no `curl`, so its health check runs the program again with the argument `healthcheck`. That call asks `/healthz` on the loopback address and exits with 0 only when the answer is 200. The memory limit of the container and the `GOMEMLIMIT` setting belong to the compose file that runs the image, and `.dockerignore` keeps the env files, the documentation and the tool settings out of the build context.

The Hadolint job lints the `Dockerfile`, and `docker.yaml` builds, scans and publishes the image.

Image tags are `latest` for `main`, `sha-<commit>` for builds of `main` and of pull requests, `pr-<number>` for pull requests, and `1.2.3`, `1.2` and `1` for version tags, where `1` is only set from a major version of 1 or higher. A release commit is built twice, once for `main` and once for its tag, so only the build of `main` sets `latest` and the `sha-` tag, and the build of the tag only adds the version tags. The image is published as `ghcr.io/aethera-deeptech/backend` with the built-in token. GitHub creates the package as private on the first push, so the owner sets it to public in the package settings before anyone can pull it or run `cosign verify` without a login. Every tag is one image for both platforms, and the signature covers the index of both. Trivy scans each platform and reports it in its own code scanning category. The SBOM is made for the amd64 image only.

Verify an image with cosign by checking the identity of the workflow that signed it.

```sh
cosign verify \
  --new-bundle-format=false \
  --certificate-identity-regexp '^https://github\.com/aethera-deeptech/backend/\.github/workflows/docker\.yaml@refs/(heads/main|tags/v[0-9]+\.[0-9]+\.[0-9]+)$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/aethera-deeptech/backend:latest
```

The pattern leaves out the identity of pull request builds on purpose, so an image tagged `pr-<number>` does not pass this check. The check inside `docker.yaml` accepts pull request identities because it only verifies the image that the same run just built. The base images come from Docker Hub, and the build does not use the Google mirror, so a pull limit of Docker Hub can fail a build. Run the build again when that happens.

## Docker Hub mirror

The Docker workflow also runs for pull requests, and a pull request can change that workflow, so the Docker Hub token does not belong in it. The token is the secret `DOCKERHUB_TOKEN` of the `docker-hub` environment, and the account name is the repository variable `DOCKERHUB_USERNAME`. The mirror workflow starts when the Docker workflow completes. GitHub runs the copy of the mirror file that is on `main`, so a pull request cannot change it. The mirror never checks out the repository and never runs anything from the pull request. It only moves an image by digest.

A person with write access can change `docker.yaml` in a pull request and push any image under any tag of GitHub Container Registry. So the mirror trusts no tag. It copies an image only when the signature on it was made by `docker.yaml` at the ref of the run that started the mirror. That ref is `main` for a push to `main`, the tag for a release, and the merge of the pull request for a pull request. A push or a manual run must also name the same commit in the certificate. Tags are `pr-<number>` for a pull request, `sha-<commit>` and `latest` for `main`, and the version tags for a release, where `1.2` and `1` only move when the release is the highest of its line. A major version of 0 has no major tag, so a `v0.x` release only gets `0.x.y` and `0.x`. A prerelease tag such as `v1.2.3-rc.1` is not copied. For a pull request the mirror skips the copy when the pull request has a newer commit, and for `main` when `main` has one.

The mirror copies the signature first, checks it on Docker Hub and only then moves `latest` or a floating version tag, so Docker Hub never holds a moved tag without a signature. The calls to Docker Hub that create or inspect a tag, and the copy of the signature, are tried up to three times, because Docker Hub now and then answers a call with a 500 that the next try does not get. The calls to GitHub Container Registry, the signature checks and the login are not retried. Two limits are accepted. The first tag of a copy, which is the `sha-` tag, the `pr-` tag or an exact version tag, is created on Docker Hub before the signature is copied and checked there. The image itself was already verified on GitHub Container Registry by its digest, and `latest` or a floating tag never moves before the check on Docker Hub. Two releases that finish within minutes of each other run in separate groups, so the run of the older release could write a floating tag such as `1.2` after the newer one. The digest check of the older run would not notice, so look at the floating tags on Docker Hub after two releases in a row. A `pr-<number>` image is only as trustworthy as its pull request, because the author can change `docker.yaml` there, so never run one as a release. The rule that only `main` and tags may use the environment does not limit this workflow, because a `workflow_run` always runs on the default branch. The signature check is what protects the tags. The release tags `v*` are also protected by the rulesets `release-tags` and `release-tags-creation`, which let only the release App create them, so a pull request cannot make a tag that the mirror would accept.

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

Renovate opens the update pull requests, and `renovate.json` holds its rules. It runs on Mondays before 07:00 in Asia/Jakarta and proposes a release only after it is 7 days old. The exception is an image from `ghcr.io`, such as zizmor, because that registry gives Renovate no release time to age, so its updates are not held by the wait. It groups minor and patch updates of Go modules (`go-minor-patch`) and of Actions (`actions`), and a major update gets its own pull request. After a Go update it runs `go mod tidy`. The Go release in `go.mod` is raised by hand, because a new patch release of Go is a decision and not an update of a package. The commit type is `chore` for Go modules and for `.node-version`, `ci` for Actions and the tools of the workflows, and `build` for the Dockerfile of the app. A routine update therefore never starts a release. The bot keeps an issue called Dependency Dashboard with every update it knows about.

Security fixes ignore the schedule and the 7 day wait. Renovate learns about them from the Dependabot alerts of the repository, so those alerts stay on. Dependabot security updates stay off, so that Renovate is the only bot that opens pull requests. A pull request that comes from such an alert gets the commit type `fix`, which does start a release.

The Renovate config job runs `renovate-config-validator --strict` on every pull request, because a broken `renovate.json` does not fail anything by itself. The bot just stops opening pull requests.

## Pinned tools

A dependency update bot cannot read a tool that is named inside a `run` step, so the tools of the jobs are listed where it can.

- `.github/tools/Dockerfile` has one stage for each image that a job starts with `docker run`, which are actionlint, zizmor, ShellCheck, Hadolint, dotenv-linter and gitleaks. Nothing builds it. The workflows get the image from `.github/tools/image.sh <stage>`, so the version of a bump is the version the jobs run.
- The versions of ls-lint and of the Renovate validator sit in the `env` of their steps. A comment such as `# renovate: datasource=npm depName=@ls-lint/ls-lint` right above the line tells Renovate where to look, so keep the comment when you edit the line.

All actions are pinned to commit SHAs with the version in a comment, and the setting "require actions to be pinned to a full-length commit SHA" is on for the repository.

## Pulling images

Docker Hub limits anonymous pulls per IP address, and every GitHub runner shares its addresses with many other users. The jobs that start a Docker Hub tool image therefore pull it through `.github/actions/pull-images`. The Zizmor image comes from ghcr.io, which has no such limit, so that job pulls it as it is.

- The action adds `mirror.gcr.io` to the registry mirrors of the Docker daemon, which serves the popular Docker Hub images without the limit.
- It then logs in to Docker Hub with a read-only token, so an image that the mirror does not have is pulled as a known account. A login that fails does not fail the job.
- `.github/actions/pull-images/pull.sh` pulls an image through the mirror first and falls back to Docker Hub.

The account name is the repository variable `DOCKERHUB_USERNAME` and the token is the secret `DOCKERHUB_PULL_TOKEN`. Without them everything still works through the mirror alone. Pull requests from forks never get the secret, so they pull through the mirror and fall back to anonymous Docker Hub pulls, which the limit can refuse.

## Releases

Versions are decided by [release-please](https://github.com/googleapis/release-please) from the Conventional Commit messages that reach `main`. A `fix` raises the patch version and a `feat` raises the minor version. A `feat!` or a `BREAKING CHANGE` footer raises the major version, and while the version is below 1.0.0 a breaking change only raises the minor version. The version started at 0.0.0 in `.release-please-manifest.json`, so the first release with a `feat` was 0.1.0. Its changelog lists the `feat` and `fix` entries of the commits on `main`.

1. Merge pull requests into `main` with a Conventional Commit title.
2. release-please opens or updates a pull request named like `chore(main): release 0.2.0` with the new version and the changelog.
3. Read the changelog and the new settings, compose files and migrations of the release. Merge the release pull request only when you want to release. Nobody and nothing merges it by itself.
4. The merge creates the tag `v0.2.0` and a GitHub release. The tag starts `docker.yaml`, which pushes the image tags `0.2.0` and `0.2`, and then the mirror copies them to Docker Hub. A major version of 0 has no major tag.

A tag or a pull request that is created with the default token of a workflow does not start other workflows. So the workflow gets its token from the GitHub App `syafa-bot` with write access to contents, pull requests and issues. With that token the release pull request runs the checks and the new tag runs the Docker workflow. The App also makes the release commits, so they carry no personal account. The client ID of the App is the repository variable `RELEASE_APP_CLIENT_ID`, and its private key is the secret `RELEASE_APP_PRIVATE_KEY` of the `release` environment. The App is the only bypass actor of the ruleset `release-tags-creation`, so nothing else can create a tag that starts with `v`. Keep the default tag format. A `component` in the configuration would make tags such as `backend-v0.2.0`, which do not match `v*.*.*`, and no image would be built.
