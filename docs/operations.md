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

The settings that a production host must set are `APP_ENV=production`, `APP_ORIGIN` (the origin of the web app with https and no path, which CORS allows and the links in the emails point to), the `DB_*` settings, and `SMTP_HOST` and `SMTP_FROM_ADDRESS` with the rest of the `SMTP_*` settings. The service refuses to start in production when `APP_ORIGIN` is not https or when no mail server is set, because without a mail server nobody can verify an email. Set `TRUST_PROXY=true` when the reverse proxy on the host sends the address of the client in `PROXY_HEADER` (`X-Real-IP` by default). The service then trusts that header only for a request that comes from a private or loopback address. Without it every client has the address of the proxy, and the limits on sign in and sign up count all clients together.

## The reverse proxy

The limits on sign in, sign up and the other account routes count clients by their address, so the address has to be right.

- The service trusts every private and loopback address to set that header, which includes other containers on a shared Docker network. Set `TRUST_PROXY_CIDRS` to the address of the proxy as the container sees it, for example the gateway of the compose network, when other apps share a network with the API.
- nginx must overwrite the header that the service reads, `proxy_set_header X-Real-IP $remote_addr;`, and must never pass the header of the client on. The service trusts the header only for a request that comes from a private or loopback address, which is nginx on the same host.
- Behind Cloudflare, `$remote_addr` is the address of a Cloudflare server unless nginx uses the realip module with the ranges of Cloudflare (`set_real_ip_from` for each range, and `real_ip_header CF-Connecting-IP;`). Without it every client shares a few addresses, and the limits lock out real users together.
- An IPv6 client is counted by its /64 prefix, because it can change the rest of its address at no cost.
- Put an nginx `limit_req` zone on `/v1/auth/` as well, so a flood is stopped before it reaches the pool of the database. The limits of the service are a second line and cost a database query for each request.
- The service accepts a body of 8 KiB on the account routes, and nginx can set `client_max_body_size` to 1m.

## Clean up

One copy at a time deletes what no rule needs any more, once an hour, and takes an advisory lock of PostgreSQL so the other copy skips the round. It removes sessions past their lifetime, used and expired email tokens, rate limit events older than two hours, and businesses whose email was not verified within 7 days together with their users. The table of the rate limits is UNLOGGED, so a count is not written to the log of the shared database, and a crash of the database empties it.

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

The migration connection has no `lock_timeout` on purpose, because PostgreSQL applies that setting to every lock wait, the advisory lock that makes a second copy wait included. A migration that changes a table that already exists starts with `SET LOCAL lock_timeout = '10s';`, so it gives up when a long query holds the table and does not block every other query behind it. Write every migration so that the version before it still works with the new schema (add first, remove in a later release). During a rolling update one copy runs the new version and applies the migration while the other still runs the old one. A copy that starts on a clean database that is ahead of it skips the migration step and runs, which is also what a rollback to an older version relies on. A dirty database that is ahead still stops the start, see the next paragraph. A migration that cannot work with the old code needs a stop and the written confirmation in the changelog.

The library marks the schema version as dirty before it runs a migration and clears the mark after it, in two separate steps. A copy that is killed halfway (an out of memory kill, or a stop that waits longer than the grace period) leaves the mark, and every later start stops with "Dirty database version". The kill can come before the migration ran, during it, or after it ran and before the mark was cleared, so the schema may or may not hold the changes of that migration. Do not guess. Look at the tables and columns of that migration file first. Take a backup first if the database holds data you cannot lose. Then fix the table `schema_migrations` of the database of the service by hand. When the changes are all there, set `dirty` to false and keep `version`. When none of them are there, set `version` to the migration before it and `dirty` to false. When the interrupted migration is the first one, there is no version before it, so delete the one row of `schema_migrations`, which is the state of a database that was never migrated. A migration that left only some of its changes needs those removed by hand first.

The compose services are called `api-1` and `api-2`, and Docker adds those names as network aliases on the shared database network. An app on that network that has a service with the same name would share the alias. Check the aliases on that network before the first deploy.

The readiness route `/readyz` answers 200 while the database answers and 503 when it does not. The container health check uses `/healthz`, which ignores the database on purpose, because a restart does not fix a database that is down. The answer of `/readyz` is reused for 2 seconds, and it should stay off the public reverse proxy, which only needs `/healthz`.
