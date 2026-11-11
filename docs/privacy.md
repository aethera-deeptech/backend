# Privacy

This page describes, from the code, which personal data the backend keeps, who can read it, how long it stays and what the operator does about deleting it. It is a technical description for the people who run the service, and the privacy policy that an owner reads is a separate text.

## What is kept

| Who | Data | Where |
|:---|:---|:---|
| An account | The email, a hash of the password, the time the email was verified, and the time and the version of the terms that the person accepted at sign up. The column `google_subject` is for a later sign in with Google and is empty today | `users` |
| A business | The name, the time zone, the tariff, the work days and hours and the anomaly sensitivity | `businesses` |
| A session | A SHA-256 hash of the cookie, never the cookie itself | `sessions` |
| A mail link | A SHA-256 hash of the token, the email it is for and its expiry | `email_tokens` |
| The devices of a business | The names of the gateways, rooms and devices, the hardware id and the key hash of each gateway, the ids of its nodes, and the times they were last seen. A pending enrollment keeps the hardware id, the firmware and hashes of the claim code | `gateways`, `gateway_enrollments`, `rooms`, `devices` and `nodes` |
| The energy of a business | Raw samples of power for 30 days, and the sums of each hour for 400 days | `readings` and `readings_hourly` |
| The work of a business | Tips and the decisions of the owner, anomalies, the pieces made each day, and the relay commands and schedules | `tips`, `anomalies`, `production_records`, `relay_commands` and `relay_schedules` |
| A client | A short hash of the client address or of the email, with the name of the limit and the time, for two hours. A short hash of an IPv4 address can be guessed, so treat it as pseudonymous | `rate_limit_events` |

The service never stores a password in clear text. A request body is not written to the access log. A session holds no address. It holds a short label of the browser and the system, such as Chrome on Windows, made from the User-Agent at sign in, and the User-Agent itself is not stored. The data of a gateway shows when the machines of a business run, so treat it as business data.

## Who can read it

- An owner reads only the business of the signed in account. An id of another business answers like an id that does not exist.
- The service has no administrator role and no route that reads another business.
- A person who runs the service can read the database and the logs. The database holds the email of every account, so only the operator should have access.

## What leaves the service

| Where | What | When |
|:---|:---|:---|
| The SMTP server that the operator configures | The email address and the link of a verification or reset mail, and the address alone for the one mail that says an account was deleted | Every mail the service sends. When `SMTP_HOST` is empty, which production refuses, the service writes the address and the link to its log instead |
| Sentry | The error text, the stack, the route pattern, the status, the request id, the id of the account, the release, the environment and the server name. The text is scrubbed of emails, tokens, addresses and long numbers, but the scrubbing is best effort, so a name that sits in a database error text can pass | Only when `SENTRY_DSN` is set, and only for a failure of the service |
| Grafana Cloud | The metrics, and the logs of the API containers. A log line of a request has the method, the path without its query, the status, the time it took, the client address and the error code. A route with an id in its path is logged by its pattern, such as `/v1/devices/:id`, so the id is not in the log. A line about a failed route has the type of the error and not its text | Only when the operator turns on the monitoring profile, see [Monitoring](monitoring.md). The operator sets how long Grafana Cloud keeps the logs |
| The container log files of the host | The same log lines | Always. The production example keeps up to 5 files of 10 MB for each container by default (`LOG_MAX_SIZE` and `LOG_MAX_FILES`), cut by size and not by age, and Docker removes them when a container is recreated |

There is no analytics script and no advertising. The service sets one cookie, the session cookie.

## How long it stays

| Data | Kept |
|:---|:---|
| Raw readings | 30 days |
| Hourly sums | 400 days |
| Anomalies | 90 days |
| Sessions | 30 days without use, and 90 days in all |
| Mail tokens | A day after they expire, and a used token for 7 days |
| Rate limit events | 2 hours |
| A business that never verified an email | 7 days, then the business and its account are deleted |
| Pairings that are no longer searching | 7 days after they start |
| Gateway enrollments | A day after they expire |
| Relay commands | 30 days |
| Everything else | Until the business is deleted |

## Deleting an account

The owner deletes the account in the app with `DELETE /v1/account`, which asks for the password again. The route first cuts off the hubs of the business, so they stop sending, and then deletes the readings, the hourly sums and the anomalies in batches that commit one by one. Then it deletes the business row, and every other table under the business goes with it in that one statement, because the keys cascade, see [Data model](data-model.md). That covers every table under the business, for example the user, the sessions, the gateways, the devices, the tips and the relay data. A request that stops half way leaves the account in place with part of its readings gone, and the next call goes on. It cannot be undone. When the delete call returns without an error, the service queues one short mail to the address of the owner that says the account is gone. It keeps the address in memory for that mail only, until the mail is sent or dropped. A failed send is tried twice more, and a mail that the queue drops is not queued a second time. The operator can still delete a business row by hand for an owner who cannot sign in. The logs, the backups, the reverse proxy of the host, Sentry, Grafana Cloud and the mail provider keep their own copies and follow their own retention.

The owner can also remove one gateway with `DELETE /v1/gateways/{id}`, which asks for the password again in the same way. It cuts the key first, deletes the readings, the hourly sums and the anomalies of that gateway in batches, and then deletes the gateway row, and its nodes, devices, tips, relay commands and schedules go with it because the keys cascade. A request that stops half way leaves a gateway with a cut key and fewer rows, and the next call goes on. It cannot be undone. The copies that the paragraph above names keep their own retention.

## Consent

A sign up counts as the consent to the terms and the privacy policy. The service stores the time and the version of the texts in `users`, so a later change of the texts can be tied to who accepted which version.
