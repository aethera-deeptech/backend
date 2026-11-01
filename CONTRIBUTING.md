# Contributing

Thanks for helping with the backend of the energy audit dashboard. This guide keeps changes easy to review.

Everyone who takes part in this project follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## Set up

The repository has no service code yet, so there is nothing to install or run. The pull request that adds the Go service will also describe how to run it. Until then a change is a change of the workflows, the configuration or the documentation.

## Making a change

1. Branch from `main`. Name the branch `type/short-description`, for example `fix/device-status`, using the same type as your commits.
2. Keep one pull request to one concern. Unrelated changes go into separate branches and pull requests.
3. Write commits in the [Conventional Commits](https://www.conventionalcommits.org/) style, as `type(scope): summary`. The summary is the whole message, so do not add a body. The types are `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore` and `revert`. A pull request cannot be merged while its title or one of its commit messages does not follow this, because the required check "Conventional commits" fails. The check skips merge commits and the messages that commitlint ignores by default, which start with `Revert`, `Reapply`, `amend!`, `fixup!` or `squash!`. Fix a message with `git commit --amend` for the last commit, or an interactive rebase for an older one, and force push your own branch.
4. Write code, comments, commit messages and documentation in English. Match the style of the code around your change.
5. Add tests with every behavior change. A bug fix should come with a test that fails without the fix.

Sign your commits with a GPG key, with an SSH key that you added to your GitHub account as a signing key, or with an S/MIME certificate from a trusted authority. The ruleset of `main` does not require a verified signature yet. It will once the release App has made its first verified commit, so sign now and your branch stays ready. The email address of the commit has to be a verified email address of your account for a GPG or S/MIME signature. GitHub explains the setup in [About commit signature verification](https://docs.github.com/en/authentication/managing-commit-signature-verification/about-commit-signature-verification). The squash and rebase buttons are off, because they would replace your signatures, so the commits keep the signatures that they have.

## Checks run in CI

Every check runs in CI on your pull request, so you should not need to run heavy tasks such as full builds or test suites on your own machine. Push your branch, open the pull request and read the results of the checks. Run something locally only when the CI logs cannot explain a failure, and keep it small.

## Pull requests

- The title is a Conventional Commit, such as `fix(devices): keep the status after a restart`. The "Conventional commits" check blocks the merge when the title or a commit that it checks does not follow it.
- The body is only the list of commits in the pull request, one hyphen bullet per commit. The check accepts lines that start with a hyphen and text, so other bullet marks and indented sub bullets fail.
- Name the branch `type/short-description`, with the type of the commits, such as `fix/device-status`.
- Add one label, `bug` for fixes, `enhancement` for features and `documentation` for documentation only, and assign the pull request to yourself.
- The "PR format" check blocks the merge when the branch name, the label, the assignee or the body does not follow this. Add what is missing and then either rerun the failed check from the checks tab or edit the body, because adding a label or an assignee does not start the check again.
- Pull requests are merged with a merge commit, never squashed or rebased.
- Every required check must pass. They are listed in the repository rules and described in [docs/ci-cd.md](docs/ci-cd.md).
- Read the bot comments on your pull request and resolve every review thread.

## Reporting problems

Use the issue templates for bugs and feature requests. Report security problems privately as described in [SECURITY.md](SECURITY.md).
