# Privacy

This page describes, from the code, which personal data the backend keeps, who can read it, how long it stays and what the operator does about deleting it. It is a technical description for the people who run the service, and the privacy policy that an owner reads is a separate text.

## What is kept

| Who | Data | Where |
|:---|:---|:---|
| An account | The email, a hash of the password, the time the email was verified, and the time and the version of the terms that the person accepted at sign up | `users` |
| A business | The name, the time zone, the tariff, the work days and hours and the anomaly sensitivity | `businesses` |
| A session | A SHA-256 hash of the cookie, never the cookie itself | `sessions` |
| A mail link | A SHA-256 hash of the token, the email it is for and its expiry | `email_tokens` |
| The devices of a business | The names of the gateways, rooms and devices, and the hardware id and the key hash of each gateway | `gateways`, `rooms`, `devices` and `nodes` |
| The energy of a business | Raw samples of power for 30 days, and the sums of each hour for 400 days | `readings` and `readings_hourly` |
| The work of a business | Tips and the decisions of the owner, anomalies, the pieces made each day, and the relay commands and schedules | `tips`, `anomalies`, `production_records`, `relay_commands` and `relay_schedules` |
| A client | An address and a key for the rate limits, for two hours | `rate_limit_events` |

The service never stores a password in clear text. A request body is not written to the access log. The data of a gateway shows when the machines of a business run, so treat it as business data.

## Who can read it

- An owner reads only the business of the signed in account. An id of another business answers like an id that does not exist.
- The service has no administrator role and no route that reads another business.
- A person who runs the service can read the database and the logs. The database holds the email of every account, so only the operator should have access.

## What leaves the service

| Where | What | When |
|:---|:---|:---|
| The SMTP server that the operator configures | The email address and the link of a verification or reset mail | Every mail the service sends |
| Sentry | The error text, the stack, the route pattern, the status, the request id and the id of the account, with the text scrubbed of emails, tokens and long numbers | Only when `SENTRY_DSN` is set, and only for a failure of the service |
| Grafana Cloud | The metrics, and the logs of the API containers, which include the client address and the path of each request without its query | Only when the operator turns on the monitoring profile, see [Monitoring](monitoring.md) |
| The container log files of the host | The same log lines | Always, and the host keeps a few files of 10 MB by default |

There is no analytics script and no advertising. The service sets one cookie, the session cookie.

## How long it stays

| Data | Kept |
|:---|:---|
| Raw readings | 30 days |
| Hourly sums | 400 days |
| Sessions | 30 days without use, and 90 days in all |
| Mail tokens | A day after they expire, and a used token for 7 days |
| Rate limit events | 2 hours |
| A business that never verified an email | 7 days, then the business and its account are deleted |
| Ended pairings and enrollments | A week |
| Relay commands | 30 days |
| Everything else | Until the business is deleted |

## Deleting an account

The API has no route that deletes an account or a business, and the dashboard cannot ask for it. Today an owner who wants the data removed asks the operator, who deletes the business row. Every table under the business goes with it, because the keys cascade, see [Data model](data-model.md). The logs and the backups of the operator are separate and follow their own retention.

## Consent

A sign up counts as the consent to the terms and the privacy policy. The service stores the time and the version of the texts in `users`, so a later change of the texts can be tied to who accepted which version.
