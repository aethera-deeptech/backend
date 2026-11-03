# Security

This page describes what the API protects, who it protects it from and what stops them. To report a problem, follow [SECURITY.md](../SECURITY.md).

## What is protected

- The accounts and passwords of the owners.
- The energy readings of a business, which show when its machines run.
- The keys of the gateways, which let a device send readings for a business.
- The availability of the API for a business that depends on it.

## Who is a threat

| Actor | What they can try |
|:---|:---|
| A stranger on the internet | Guess passwords, flood the public routes, send malformed or hostile requests |
| A signed in owner | Read or change the data of another business |
| A page of another site | Make the browser of an owner send a request with the cookie |
| Someone with a stolen session, key or link | Reuse a session cookie, a gateway key or a mail link |
| A supply chain attacker | Slip a bad dependency, base image or workflow into the build |

## Threats and measures

| Threat | Measure |
|:---|:---|
| Guessing a password | A password is hashed with argon2id, with a cost that follows the OWASP guidance. Only a few hashes run at once, and a request that finds no free slot answers 503 `busy`. A sign in with an unknown email spends the same time as a wrong password, and the answer is the same |
| Flooding the account routes | Each route has a limit for the client address and for the email, for example 10 sign ins for an email in 15 minutes. The counters live in PostgreSQL, so every copy of the service shares them. A flood with a new email each time is limited by the address only, and the proxy has to limit it too |
| Reading another business | Every query is limited to the business of the signed in account. An id of another business answers 404 like an id that does not exist, so the answer does not tell whether it exists |
| A stolen session | The cookie is `HttpOnly` and `SameSite=Lax`, and it is `Secure` with the `__Host-` prefix in production. The service stores only the SHA-256 hash of a session. A session ends after 30 days without use or 90 days in all, an account keeps at most 20, and a password reset ends every session of the account |
| A forged request from another site | The CORS rule allows only the origin of the app. A request that changes data also needs the header `X-Requested-With`, and its `Origin` must be the origin of the app when the browser sends one. A page of another site cannot add the header without a preflight, and the preflight is refused |
| A stolen gateway key | The service stores only the SHA-256 hash of the key, so a copy of the database cannot act as a gateway. A gateway can send 600 batches an hour, and a batch has a sequence number, so a replay is acknowledged again and does not write twice |
| A mail link that leaks | A verification link lives 24 hours and a reset link one hour. The service stores only the hash of a token, a link works once, and the reset answer is 202 for every email, so it does not show which emails have an account |
| A hostile body | The JSON reader refuses an unknown field, a second value and a body over the limit, and every field is validated before a rule runs. A request is limited to 288 KiB, a batch to 256 KiB and an enroll request to 2 KiB, and the server has read and write timeouts of 15 seconds |
| A browser that treats data as a page | Every answer has `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and a content security policy that allows nothing. The answers under `/v1` are not stored by a proxy or a browser |
| Personal data in logs and error reports | The access log has the method, the path without the query, the status and the address, and never a header or a body. An error report has the error text, the stack and the route pattern, and every text is scrubbed of emails, bearer tokens, long keys and long numbers |
| A flood of the database | The pool has a fixed size, and the health route reuses the answer of the database check for two seconds, so a flood of probes costs one ping |
| A broken or leaked credential | Settings come from environment variables and never from a file in the repository. Secret scanning with push protection and gitleaks look for a secret in every change. The Docker Hub token and the release key live in environments that only `main` and version tags can use |
| A supply chain attack | Every action is pinned to a commit. CI runs CodeQL, a vulnerability check, a license check, dependency review and Trivy. The image is built by one workflow, has an SBOM attestation, and is signed with cosign. [CI/CD](ci-cd.md) lists the checks |

## What is not covered

- The proxy and the host. The service listens on the loopback address and expects a reverse proxy in front of it. Limiting a flood from many addresses, terminating TLS and keeping the host patched belong to the operator, see [Operations](operations.md).
- A stolen owner account. Whoever holds the password and the mailbox is the owner for the service.
- The rate limit tables can be emptied by a crash of the database, because they are unlogged. The limits then start over.
- Nothing here has had a penetration test by a third party. The [ZAP scans](ci-cd.md) check for the usual web faults and are not a substitute.
