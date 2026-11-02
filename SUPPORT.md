# Support

The backend is an open source project that is maintained by its owner, and there is no paid support or response time promise. This page says where to look first and where each kind of question belongs.

## Look here first

| You want to know | Read |
|:---|:---|
| What a route takes and returns, with every field, limit and status | [openapi.yaml](openapi.yaml) and [API](docs/api.md) |
| How a gateway enrolls, sends readings and takes relay commands | The [firmware guide](docs/firmware-guide.md) |
| How to run the stack, what each setting does and how to deploy | The [README](README.md) and [Operations](docs/operations.md) |
| How the metrics, the logs and the alerts work | [Monitoring](docs/monitoring.md) |
| What each workflow and required check does | [CI/CD](docs/ci-cd.md) |
| How to build, test and send a change | [CONTRIBUTING.md](CONTRIBUTING.md) |

## Where each question goes

- **A bug.** Open an issue with the [bug report form](https://github.com/aethera-deeptech/backend/issues/new?template=bug_report.yml). Say what happened, the steps to reproduce it, the version from `/healthz` and the logs or responses, with secrets and personal data removed.
- **An idea or a missing feature.** Open an issue with the [feature request form](https://github.com/aethera-deeptech/backend/issues/new?template=feature_request.yml).
- **A security problem.** Never open a public issue. Use the private report described in [SECURITY.md](SECURITY.md).
- **A barrier in the documentation or the API for a person with a disability.** Follow [ACCESSIBILITY.md](ACCESSIBILITY.md).
- **A question about the dashboard.** Ask in the [frontend repository](https://github.com/aethera-deeptech/frontend), which holds the screens that call this API.
- **A change you want to make.** Read [CONTRIBUTING.md](CONTRIBUTING.md) first. A commit message or a pull request title that does not follow Conventional Commits blocks the merge.

## What is not supported

Running and securing your own deployment is your responsibility, and this repository cannot debug it. The [operations page](docs/operations.md) lists what to check, and an issue is welcome when it turns out to be a bug in the code. Only the latest release and `main` are supported.
