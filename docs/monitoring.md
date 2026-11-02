# Monitoring

This page explains how the metrics and the logs of the API reach Grafana Cloud, what the repository ships for it and how to set it up. Nothing here is needed to run the API, and the stack works the same without it. The files are ready, and the Grafana Cloud token is added by the owner when the stack is created.

## How it fits together

The API serves its metrics on a second listener, `METRICS_ADDRESS`, such as `:9100`, which the compose file never publishes on the host. Alloy runs next to the API on the `internal` network of the compose file. It finds the containers to watch by the label `aethera.role`, scrapes the metrics listener of each API container every minute and reads the logs of the same containers. It sends both to Grafana Cloud. Because it goes by label and not by name, the containers can have any name, and a second copy of the API needs no change to the Alloy config. The metrics are described in [Operations](operations.md).

Alloy has to list containers to find their logs, which needs the Docker socket. Mounting the socket in Alloy would give a compromised Alloy the control of the host, so the socket goes to a proxy instead. The `socket-proxy` container answers read requests for containers and networks and refuses everything else, and it sits on a network with no route out that only Alloy shares. A compromised Alloy therefore cannot start, stop or create a container. It can still read every container on the host, which includes the environment of a container with its secrets and the files of its filesystem, because the proxy opens every read request under `/containers` and does not limit which containers it shows. Alloy is trusted at the level of the other containers of the stack. Both containers belong to the `monitoring` profile, so they do not start unless that profile is on.

## More than one copy or host

Every metric and log line carries two labels that say where it came from. `host` is the name of the host, from `GRAFANA_HOST` in `grafana/alloy/.env`, and `instance` is the name of the container. The dashboards have a `Host` and an `API copy` variable at the top, and the alerts that watch one copy raise one alert for each copy and name it.

To run a second copy of the API on the same host, give its service the same two labels as the `api` anchor in `compose.production.example.yml`, `aethera.role: api` and `aethera.metrics_port`. Alloy picks it up within a minute. The copy has to be on the `internal` network, which Alloy shares. To add another host, run the same compose file with the `monitoring` profile there, with its own `grafana/alloy/.env` and a `GRAFANA_HOST` that no other host uses. Two hosts with the same name would mix their series.

## What the repository holds

| Path | What it is |
|:---|:---|
| `grafana/alloy/config.alloy` | The Alloy pipeline for the metrics and the logs |
| `grafana/alloy/.env.example` | The settings of Alloy, which go into `grafana/alloy/.env` |
| `grafana/dashboards/*.json` | API overview, Routes and Logs, each with the `Host` and `API copy` variables |
| `grafana/alerts/rules.json` | The alert rules, as an array in the format of the alert rule provisioning API |

The dashboards pick their data sources with two variables at the top (`Metrics` and `Logs`), so they hold no data source id. The alert rules hold the placeholder `__PROMETHEUS_UID__` in place of the id of the metrics data source, and it has to be replaced before they are applied. Grafana is not synced from this repository. The JSON files are the record of what was built, and a change made in Grafana is copied back here by hand.

## Set it up

1. In the Grafana Cloud stack, open the connection details of the Prometheus and the Loki data source and note the push URL and the user (instance id) of each.
2. Create an access policy for the stack with the scopes `metrics:write` and `logs:write`, then create a token for it. The token is shown once.
3. Copy the `grafana` folder to the host next to `compose.yml`.
4. Copy `grafana/alloy/.env.example` to `grafana/alloy/.env` on the host, which git ignores, and fill in the `GRAFANA_CLOUD_*` values and `GRAFANA_HOST`. Only Alloy loads that file, so the API copies never see the token. Add `COMPOSE_PROFILES=monitoring` and `METRICS_ADDRESS=:9100` to the root `.env`.
5. The API copies read `METRICS_ADDRESS` when they start, so recreate them one at a time with the version that runs now, for example `./scripts/rolling-update.sh 0.3.0`. The script checks the health of each copy before it goes on to the next.
6. Start the two new containers and check that Alloy runs.

   ```sh
   docker compose up -d alloy socket-proxy
   docker compose logs --since 5m alloy
   ```

7. After a few minutes the Explore view of the Prometheus data source answers `up{service="aethera-api"}` with `1`, and the Loki data source answers `{service_name="aethera", role="api"}` with lines.
8. Set the scrape interval of the Prometheus data source to `1m`, which is how often Alloy scrapes, then import the dashboards from `grafana/dashboards` and choose the two data sources when asked.
9. Create a contact point that reaches the owner and a notification policy that uses it, then create the alert rules. Replace `__PROMETHEUS_UID__` in `grafana/alerts/rules.json` with the id of the Prometheus data source and send each rule to the provisioning API. A folder with the id `aethera` has to exist first.

Alloy keeps a small write-ahead log in the `aethera-alloy` volume, so a short outage of the network does not lose metric samples. The script `scripts/rolling-update.sh` recreates the API copies only, so a deploy does not restart Alloy.

## Keep it inside the free plan

The Grafana Cloud free plan allows about 10 000 active series and 50 GB of logs a month. The API has a few hundred series for each copy, mostly the buckets of the latency histogram for each route. The logs are the larger cost, because every request is one line.

## What the alerts watch

| Alert | Fires when |
|:---|:---|
| API is down | A copy that runs did not answer its scrape for 5 minutes. It also fires when no data arrives at all, which is what happens when Alloy is down. A copy that was stopped or removed disappears from the discovery, so "Fewer than 2 API copies are up" is the rule that catches it |
| Fewer than 2 API copies are up | Fewer than 2 copies of the API reported `up` for 5 minutes. Removing a copy on purpose fires it until the threshold in the rule is lowered |
| 5xx ratio is high | More than 5 percent of the requests failed with a 5xx for 10 minutes |
| p95 latency is high | The p95 latency of the requests other than the health check was above one second for 10 minutes |
| Database pool is nearly full | More than 85 percent of the connections of the pool of a copy were in use for 5 minutes |
| Many requests are rate limited | More than 100 requests were refused by a limiter in 10 minutes |
| An API copy keeps restarting | A copy started more than once in 30 minutes, which is what a crash loop or an out of memory kill looks like. A deploy followed by a rollback within 30 minutes also fires it |
| API memory is close to its limit | The resident memory of a copy was above 110 MiB for 5 minutes. The default limit of a copy is 128 MiB, so set another threshold in the rule when `API_MEMORY_LIMIT` is not the default |
