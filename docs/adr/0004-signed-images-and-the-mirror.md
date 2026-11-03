# 0004 Images are signed by the workflow, and the mirror trusts no tag

Status: Accepted, 2026-11-01

## Context

The image is built for pull requests, for `main` and for version tags. A person with write access can change `docker.yaml` in a pull request and push any image under any tag of GitHub Container Registry. The same workflow also has to run for pull requests, because the checks need the build. Docker Hub is where the server pulls from, and its token must never be reachable from a pull request.

## Decision

`docker.yaml` builds the image and pushes it to GitHub Container Registry with the built-in token only. It scans each platform with Trivy, makes an SBOM, and signs the digest with cosign without a key, so the certificate names the workflow and the ref that built it. `resign.yaml` signs an image that exists again, from `main` only.

A second workflow, `docker-hub-mirror.yaml`, starts when the Docker workflow completes. GitHub runs the copy of that file that is on `main`, so a pull request cannot change it. It never checks out the repository. It copies an image to Docker Hub by digest, and only when the signature names `docker.yaml` at the ref of the run that started the mirror. It copies the signature first, checks it on Docker Hub, and only then moves `latest` or a floating version tag.

## Consequences

- The Docker Hub token is a secret of the `docker-hub` environment and is not in the Docker workflow.
- A person can verify an image with `cosign verify` and the identity of the workflow, as [CI/CD](../ci-cd.md#container-image) shows. An image tagged `pr-<number>` does not pass that check on purpose, and it is not a release.
- The mirror also copies the images of pull requests of this repository as `pr-<number>`. They are not release artifacts, and the deploy never uses them.
- The first tag of a copy, which is the `sha-` tag, the `pr-` tag or an exact version tag, exists on Docker Hub for a short time before its signature is there. The image itself was verified on GitHub Container Registry by its digest first, and `latest` never moves before the check on Docker Hub. This limit is accepted.
- The build pulls its base images from Docker Hub without the mirror of the other jobs, so a pull limit can fail a build. Running it again is the fix.
