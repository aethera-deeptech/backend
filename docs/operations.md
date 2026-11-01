# Operations

This page describes how the API runs on the server and how to update it.

## How it runs

Two copies of the API run in Docker on one host, `api-1` and `api-2`. Each copy listens on a port of the loopback address, 8010 and 8011 by default, so only the reverse proxy on the host can reach them. The reverse proxy lists both as its upstream and stops sending requests to a copy that is down. That is what keeps the site up while one copy restarts.

`compose.production.example.yml` defines the two copies. Every container runs with a read-only file system, no Linux capabilities and a limit on memory and CPU. The image is `syafa/aethera-backend` on Docker Hub, which a workflow copies from GitHub Container Registry after it checks the signature, see [CI/CD](ci-cd.md#docker-hub-mirror).

Both copies also join the Docker network of the shared PostgreSQL container, which compose declares as external and never creates or removes. Its name is `DB_NETWORK`, `postgres_postgres` unless it is set. The service connects with the `DB_*` settings of the `.env` file, so the database host is the name of the container on that network.

## Set up a host

1. Make a folder for the project, for example `/home/<user>/docker/aethera-backend`.
2. Copy `compose.production.example.yml` into it as `compose.yml`, copy `scripts/rolling-update.sh` next to it, and make the script executable.
3. Copy `.env.example` to `.env` and set the values of the host, which includes `APP_ENV=production`. The settings that start with `API_`, `GOMEMLIMIT`, `LOG_` and `IMAGE_TAG` belong to Docker Compose, and the others belong to the service. Keep the file readable only by its owner, with `chmod 600 .env`.
4. Put the loopback ports of the copies in the upstream of the reverse proxy, for example `server 127.0.0.1:8010;` and `server 127.0.0.1:8011;`.
5. Run `./rolling-update.sh <version>` with the first version, for example `./rolling-update.sh 0.1.0`.

The `.env` file of a host holds only the values of this service. Do not copy values from another project onto the host.

## Update the copies

Run `./rolling-update.sh <version>` on the host, in tmux or screen so a dropped connection does not stop it half way. The script does the following.

1. It checks that both services exist.
2. It pulls the image of the version, which also proves that the tag exists. When the pull fails, nothing is changed.
3. It sets `IMAGE_TAG` in `.env` to the version.
4. It recreates `api-1` alone, waits until `/healthz` answers with status `ok` and the version, twice in a row, and only then goes on to `api-2`.

When a copy does not become healthy, the script stops and says which copies run the new version and which still run the old one, and it warns that a plain `docker compose up -d` would now recreate every copy on the new version. It rolls nothing back by itself. `DRY_RUN=1` prints the steps and changes nothing, and `TIMEOUT` sets how many seconds a copy gets to become healthy, 120 by default.

To go back to an older version, run `ROLLBACK_OK=<version> ./rolling-update.sh <version>`. The extra word is a confirmation, because a newer version may have changed the database in a way that the older one cannot run on. The service has no migrations yet. Read the changelog before going back once it has.

## Check a copy

- `docker compose ps` shows the state and the health of both copies.
- `curl http://127.0.0.1:8010/healthz` answers `{"status":"ok","version":"..."}` for the first copy, and port 8011 for the second.
- `docker compose logs api-1` shows the logs of a copy. They are JSON lines, one object for each line.

## Do not do these

- Do not run `docker compose down -v`, `docker volume rm` or `docker system prune` on a host that shares Docker with other projects.
- Do not change another project's folder, containers or database from here.
- Do not put the `.env` file in a place that other users can read.

## Backups

The service applies its migrations when a copy starts, before the copy listens. The migration library holds a lock in PostgreSQL while it runs, so two copies that start together do not race. A migration that fails stops the copy and the rolling update stops with it. The service never drops a table or deletes rows in a migration without a note in the changelog. A database that holds data needs a backup that you control. Nothing in this repository makes one.

The readiness route `/readyz` answers 200 while the database answers and 503 when it does not. The container health check uses `/healthz`, which ignores the database on purpose, because a restart does not fix a database that is down.
