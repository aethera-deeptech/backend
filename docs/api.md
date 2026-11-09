# API

The contract is `openapi.yaml` in the root of the repository. The job `OpenAPI lint` checks it on every pull request. This page explains the decisions behind it. A change of a path, a field or a code changes `openapi.yaml` in the same pull request.

## Clients

- The web app of a business calls `/v1` with a session cookie. The shapes follow `docs/api-assumptions.md` of the frontend repository. This file adds routes and fields where the frames of the design need them, and it says so on each one.
- The gateway of a business calls `/device/v1` with a bearer key. A gateway is the only unit that talks to the service. The nodes (plugs and clamps) send their readings to it over ESP-NOW.

## Sessions and cookies

- The session is an opaque random token in the cookie `__Host-aethera_session`. The cookie is HttpOnly, Secure and SameSite=Lax, with no Domain and with Path `/`. The service stores a hash of the token and never the token.
- The app and the API sit on the same site, so the browser sends the cookie with a request that has credentials. CORS allows only the exact origin of the app.
- A request that changes data needs the header `X-Requested-With: aethera`, and the service checks the `Origin` header. A page of another origin cannot send that header without a preflight, so a forged form post fails.
- A session lasts 30 days from its last use and 90 days at most. A reset of the password ends every session of the user.
- `GET /v1/auth/sessions` lists the devices that are signed in, and `DELETE /v1/auth/sessions/{id}` ends one. The label of a device is the browser and the system from the User-Agent at sign in, and the service stores no address. The session of the request comes first in the list. Ending it works like logout. An id of another user answers 404.
- `POST /v1/auth/sessions/revoke-others` ends every other session of the user and keeps the one of the request. It answers 200 with `{"ended": n}`, and `n` is 0 when there was no other session.
- The service has no Redis. Sessions, limits and tokens live in PostgreSQL, so both copies see the same state.

## Deleting the account

`DELETE /v1/account` takes `{"password": "..."}` and deletes the business with everything under it, then clears the cookie and answers 204. The password is checked again so that a stolen session cannot do it alone, a wrong one answers 403 `invalid_credentials` and not 401, and a user has 5 tries an hour. The readings, the hourly sums and the anomalies go first in batches, and the business row goes last. A 503 `busy` can therefore come after a part of the readings was deleted, or after the whole delete when the time ran out just after the commit. The same call can be repeated until it ends with 204. A 401 on the repeat means that the session is gone, which a finished delete causes, and so does a session that ended in the meantime. The app cannot tell the two apart from the answer, so it sends the user to the sign in page. A sign in with the right password works only while the account still exists, and a failed sign in proves nothing, because a wrong password gives the same answer. It cannot be undone.

## Errors

Every error of a route under `/v1` or `/device/v1` is JSON with `code` and `message`, and [Error codes](errors.md) lists them all. The routes `/healthz` and `/readyz` have a small status body of their own, and they answer 503 when the service is not healthy. The app reads the `code` and writes its own text. A field that failed validation is named in `fields` with a code. A response of 429 has `Retry-After` and `retryAfterSeconds`. The codes of the account screens are `invalid_credentials`, `email_not_verified`, `google_failed`, `email_taken`, `token_invalid_or_expired`, `resend_too_soon` and `rate_limited`.

## The account and the enumeration trade-off

The design shows "Email sudah terdaftar" on the sign up screen. That text tells anyone that an email has an account. The sign in and the forgot password routes do not reveal it, and `forgot-password` and `resend-verification` answer the same for every email. The sign up keeps the text of the design and has a limit for each client address and for each email. Decide with the owner of the product whether to change the sign up to a neutral answer.

## Time

The service stores every moment in UTC and keeps an IANA time zone for each business, `Asia/Jakarta` by default. The zone decides the day, the month and the working hours. A moment in an answer carries the offset of that zone.

## Money

An amount is a whole number of rupiah. A tip shows its saving rounded to the nearest 1000 rupiah, and the report sums the rounded values, so the numbers of the screens add up. The default tariff is 1444 rupiah for each kWh.

## Devices

1. A new gateway makes a random key of its own and calls `POST /device/v1/enroll` with the SHA-256 of the key. The answer holds a claim code and a poll id, and the gateway shows the code. The service never sees the key.
2. The owner types the code in the app (`POST /v1/gateways`). The gateway polls and learns that it belongs to a business.
3. From then on the gateway sends its key as a bearer token. The service keeps only the hash of the key, so a copy of the database cannot act as a gateway. TLS protects the key in transit, and the sequence number of a batch makes a retry safe. A signature on every request would add nothing over TLS here, because a party that sees the plain request also sees the key.
4. The owner can cut off the key of a gateway with `POST /v1/gateways/{id}/revoke-key`, which answers 204. The old key gets 401 `invalid_gateway_key` from then on, and the devices and readings stay. The list shows `keyRevoked` as true until the hardware enrolls again with a new key and the owner claims the new code. The route returns no claim code, because the gateway makes its own, and it removes a claim code that waits for the same hardware, so the gateway has to enroll again with a new key. An id of another business answers 404.
5. The owner starts a pairing in the app. The next unclaimed node of that kind that the gateway reports becomes the device.

The list and the detail of a device carry `kind`, which is `plug` or `clamp`, so the app can show the relay switch only on a plug. `kindLabel` is the text for the screen and is not meant to be parsed.

Raw readings are kept 30 days. Hourly sums keep the number of samples, the sum, the highest and the lowest power and the energy of each node for 400 days, so a short spike stays visible after the raw rows are gone. The energy of an hour comes from the counter of the node when the gap to the last sample is at most one hour and the step is plausible. Otherwise it is the power times the time, with a gap of at most 5 minutes counted for each sample. A time zone with an offset that is not a whole number of hours is refused in the settings, because the hourly sums would not line up with a local day.

## The status of a device

- A device is `offline` when it has no reading or its newest reading is older than 15 minutes, and offline wins over every other status. A device with an anomaly in the last 24 hours is `anomali`. Otherwise the power decides. Below 1,2 W it is `mati`, below its working level it is `siaga`, and from there it is `nyala`.
- The working level is 20 percent of the usual high power of the device, which is the `p95` of its highest power of each hour in the last 7 days, and never below 8,5 W.
- The detail of a device draws the last 24 hours in 96 points of 15 minutes. A point is the highest power of its 15 minutes, and it is null when the device had no reading in that time, so a gap in the data, such as an outage of the gateway, is not shown as a device that was off. The expected band is the mean of the same hour of the day on earlier days plus or minus a multiple of the standard deviation. The multiple is 4, 3 or 2 for the sensitivity `rendah`, `sedang` and `tinggi`. The band is left out when any of the 24 UTC hours has fewer than 3 earlier days of data, so a device with a short history shows the chart without a band.
- The detail of a device also has `energySince`, a total that the owner can start again with `POST /v1/devices/{id}/energy-reset`. A reset sets the time of the reset to now for a plug or a clamp and deletes no reading. The total is the energy of the whole hours from the reset on, so it begins at the next whole hour, and with no reset it is the energy of every hour that is kept. The day, the month, the bill, the tips and the reports do not change with a reset. A second reset replaces the time, and the old time cannot be brought back.

## Anomalies

- A surge is a sample above 100 W that is over the usual high power of its UTC hour plus the larger of 100 W and 30 percent of that high power. This usual high power is not the `p95` that sets the working level. It is the mean of the highest power of that UTC hour on the last 15 days, plus the sensitivity multiple of the business times their standard deviation. It needs at least 7 days of history for that hour. An overload is the code 3 that the node reports. The service checks both when it stores a batch, only for a node that a business claimed and only for samples that are at most 2 hours old.
- A degradation is a motor that runs under load with a poor power factor. A batch of a node raises it when it holds at least 3 samples of 35 W or more that report a power factor, and at least 80 percent of those samples are under 0,55. The condition code 2 of the node raises it as well, the same way code 3 raises an overload. The anomaly keeps the mean power factor of the poor samples, and the device detail shows it as `anomaly.powerFactor`. The idle power factor of a motor is low anyway, which is why a load under 35 W never counts. The limits are fixed numbers and not relative to the usual load of the device, so a bank of lights or a power supply above 35 W that has a poor power factor can raise it every 6 hours. The floor of 35 W lets a small motor count, and it also lets a light load with a poor power factor count. Tune the numbers with real data before the pilot. The detail also shows `anomaly.kind`, which is `surge`, `overload` or `degradation`.
- The same node raises one anomaly of each kind in 6 hours. The one exception is an overload after a command that switched the plug on and that the plug applied, because that overload is a new cut and not a repeat. A failure of the check is logged and rolled back in a savepoint, so it does not lose the readings. When the savepoint itself fails, the batch fails and the gateway sends it again, which the sequence number makes safe.

## Tips and the check

- A tip is made for a device that has at least 24 hours of data outside the work hours in the last 7 days and drew on average at least 1,2 W in them. At 8,5 W or more it is an after-hours tip, and below that it is a standby tip. The saving is the average energy of a day outside the work hours over the last 7 days, times 30 days and the tariff, rounded to 1000 rupiah. The smallest tip is 5000 rupiah, and a device has at most one tip of each kind. A tip that nobody decided on goes away when its device no longer qualifies.
- The tips are worked out again when the app reads them, at most every 10 minutes for a business. The tips of the last run are served when the work fails.
- The owner can apply, defer, reject or cancel a tip, and the service allows only these changes. Apply and reject work from pending and deferred, defer works from pending, and cancel brings an applied tip back to pending. A verified or rejected tip is final. An applied tip is checked after 14 days against the 7 days before it. The check compares the energy for each piece of the whole business, and it counts only the days that have a production record. A tip with no records stays applied. The check says nothing about which tip caused a change, because the measure covers the whole business.

## Home and report

The home screen has the phases `no-devices`, `collecting` and `ready`. The bill comes from the measured energy only, so the load that no device measures is not in it. The report of a month sums the energy, the bill and the measured saving, lists the top devices and gives the same page as one A4 PDF. The saving has three groups, the verified ones, the ones under check and, for the running month, the ones that wait for a decision. Every month is priced with the tariff of today, because the service keeps no tariff history.

The same route and the same PDF also give the report of a week. A week is an ISO 8601 week such as `2026-W37`, and it runs from Monday to Sunday in the time zone of the business. The answer has `granularity` (`month` or `week`) and the list `weeks` next to `periods`, so the app can offer both. The bill and the top devices of a week are counted over its seven days, and the week has `through` while it is the running week. The three saving sums are rates for 30 days, as they are in a month, because a tip keeps its saving for 30 days and the service does not scale it to a week. So a week can show a saving that is larger than its bill, and the PDF says "Hemat per bulan" for that reason. A business that is still collecting data has an empty `weeks` list and answers 404 for a week. The `weeks` list goes back to the first data of the business, at most 58 weeks, which is what the hourly sums cover. The oldest week of the list can hold only part of its seven days, because the data starts in the middle of it or the hourly sums of its first days were already deleted, and the same holds for the oldest month. Such a week has no marker, so the app should not read its totals as a full week. The tip groups of an old week follow the same rule as an old month, and the waiting group is only in the running week. The PDF of a week is named `Laporan-Minggu-<week>.pdf`.

## Relay control

A plug has a relay that the owner can switch on and off in the app, or on a daily schedule. The service never calls a gateway, because a gateway sits behind a router and only calls out. So a command waits in the service until the gateway fetches it.

1. The owner calls `POST /v1/devices/{id}/relay` with `on` or `off`. The answer 202 holds the id of the command, and a command that still waits is replaced.
2. The gateway sends its next batch of readings, and the answer holds `commands`. Each command has an id, the node, the action and the time at which it expires. The gateway switches the relay.
3. The gateway reports the result in `results` of a later batch, with the same id and `ok` true or false. The command comes again in every answer until the gateway reports a result, so the gateway runs each id once. A sample can also hold `r`, the state of the relay, and the service keeps the newest one.
4. The app reads `GET /v1/devices/{id}/relay` for the state, the command that waits and the last one that ended.

The relay view has `gatewayOnline`, which is true when the gateway of the plug had a batch stored in the last 15 minutes, the same time limit as the status offline of a device. It describes the gateway and not the plug, and the status of a device follows the last reading of its own node, so the two can differ. A command is queued also when it is false, so the app can warn that the command may expire. A command that no gateway fetched in 10 minutes expires, so a plug is never switched on an old order, for example after the gateway was off all night. A gateway that has no node to report cannot send a batch, and so it cannot fetch commands, because a batch needs at least one node. A clamp has no relay and answers 409 with the code `no_relay`. A business can send 60 commands in an hour. Only a command that was queued counts, so a call that is refused before it is queued, for example with a 404 or a 409, does not use it up.

The relay view also has `cutByDevice`. A plug that carries too much load cuts its own relay and reports the code of an overload, and the view then shows `{"reason": "overload", "at": ...}` while the relay is off. It is the newest overload of the plug that is newer than the moment the gateway fetched the last command that the plug applied, and not older than 24 hours. The field is null while the state is on, and a later command that the plug applied ends it. It is an indication and not proof, because it only says that the relay is off and that a qualifying overload exists. The overload stays on record, so if someone switches the plug off by hand within 24 hours, the same cause shows again. The service cannot see a hand or a restart, so a plug that someone switched off by hand, or that restarted with its relay off, after the overload still shows the cause until then. An overload within 6 hours of the one before it is not stored again, unless a command that switched the plug on was fetched by the gateway and applied after the first one and before the new one. Then the new overload is stored, so a plug that the owner switched on and that cut itself again shows the new cause. The field is null in every other case.

The schedule is `PUT /v1/devices/{id}/schedule` with `offAt`, an optional `onAt` and `days`. The times are local times of the business. One round of the service looks at all schedules every minute, and only one copy does it at a time because the round takes an advisory lock. A schedule makes a command like the one of the owner, so it has the same delay and the same expiry. It never acts on a time that passed before it was set, or more than 10 minutes ago, which is the life of a command, so a service that was down for an hour does not switch a plug on an old order. The service does not know that the owner switched the plug by hand after the time of the schedule, so a schedule can switch it again at its next time. The schedule and the owner share the relay, a device has at most one open command, and the newest command is the one that counts. A batch may carry at most 64 `results`.

During a rolling update the copy that still runs the old version answers a batch that has `r` or `results` with 422, because it does not know the fields. The gateway sends the batch again, and the new copy takes it. A new service with an old gateway works, because the new fields are additive.

The tips only suggest, and the text of the report says so. The relay switches only on a command of the owner or on a schedule that the owner set.
