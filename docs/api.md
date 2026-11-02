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
- The service has no Redis. Sessions, limits and tokens live in PostgreSQL, so both copies see the same state.

## Errors

Every error is JSON with `code` and `message`. The app reads the `code` and writes its own text. A field that failed validation is named in `fields` with a code. A response of 429 has `Retry-After` and `retryAfterSeconds`. The codes of the account screens are `invalid_credentials`, `email_not_verified`, `google_failed`, `email_taken`, `token_invalid_or_expired`, `resend_too_soon` and `rate_limited`.

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
4. The owner starts a pairing in the app. The next unclaimed node of that kind that the gateway reports becomes the device.

Raw readings are kept 30 days. Hourly sums keep the number of samples, the sum, the highest and the lowest power and the energy of each node for 400 days, so a short spike stays visible after the raw rows are gone. The energy of an hour comes from the counter of the node when the gap to the last sample is at most one hour and the step is plausible. Otherwise it is the power times the time, with a gap of at most 5 minutes counted for each sample. A time zone with an offset that is not a whole number of hours is refused in the settings, because the hourly sums would not line up with a local day.

## The status of a device

- A device is `offline` when its newest reading is older than 15 minutes, and offline wins over every other status. A device with an anomaly in the last 24 hours is `anomali`. Otherwise the power decides. Below 1,2 W it is `mati`, below its working level it is `siaga`, and from there it is `nyala`.
- The working level is 20 percent of the usual high power of the device, which is the 95th percentile of its highest power of each hour in the last 7 days, and never below 8,5 W.
- The detail of a device draws the last 24 hours in 96 points of 15 minutes. The expected band is the mean of the same hour of the day on earlier days plus or minus a multiple of the standard deviation. The multiple is 4, 3 or 2 for the sensitivity `rendah`, `sedang` and `tinggi`.

## Anomalies

- A surge is a sample above 100 W that is over the usual high power of its UTC hour plus the larger of 100 W and 30 percent of that high power. It needs at least 7 days of history for that hour. An overload is the code 3 that the node reports. The service checks both when it stores a batch, only for a node that a business claimed and only for samples that are at most 2 hours old.
- The same node raises one anomaly of each kind in 6 hours. A failure of the check is logged and rolled back in a savepoint, so it does not lose the readings. When the savepoint itself fails, the batch fails and the gateway sends it again, which the sequence number makes safe.

## Tips and the check

- A tip is made for a device that uses power outside the work hours. The saving is the average energy of a day outside the work hours over the last 7 days, times 30 days and the tariff, rounded to 1000 rupiah. The smallest tip is 5000 rupiah, and a device has at most one tip of each kind. A tip that nobody decided on goes away when its device no longer qualifies.
- The tips are worked out again when the app reads them, at most every 10 minutes for a business. The tips of the last run are served when the work fails.
- The owner can apply, defer, reject or cancel a tip, and the service allows only these changes. Apply and reject work from pending and deferred, defer works from pending, and cancel brings an applied tip back to pending. A verified or rejected tip is final. An applied tip is checked after 14 days against the 7 days before it. The check compares the energy for each piece of the whole business, and it counts only the days that have a production record. A tip with no records stays applied. The check says nothing about which tip caused a change, because the measure covers the whole business.

## Home and report

The home screen has the phases `no-devices`, `collecting` and `ready`. The bill comes from the measured energy only, so the load that no device measures is not in it. The report of a month sums the energy, the bill and the measured saving, lists the top devices and gives the same page as one A4 PDF. The saving has three groups, the verified ones, the ones under check and, for the running month, the ones that wait for a decision. Every month is priced with the tariff of today, because the service keeps no tariff history.
