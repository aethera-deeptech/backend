# API

The contract is `openapi.yaml` in the root of the repository. The job `OpenAPI lint` checks it on every pull request. This page explains the decisions behind it. A change of a path, a field or a code changes `openapi.yaml` in the same pull request.

## Clients

- The web app of a business calls `/v1` with a session cookie. The shapes follow `docs/api-assumptions.md` of the frontend repository. This file adds routes and fields where the frames of the design need them, and it says so on each one.
- The gateway of a business calls `/device/v1` with a signed request. A gateway is the only unit that talks to the service. The nodes (plugs and clamps) send their readings to it over ESP-NOW.

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

1. A new gateway calls `POST /device/v1/enroll`, which answers a claim code and a poll id. The gateway shows the code.
2. The owner types the code in the app (`POST /v1/gateways`). The gateway polls and receives its key once. The service stores only a hash of the key.
3. From then on the gateway signs every request. The scheme is in `openapi.yaml` under `gatewaySignature`. The time stamp and the nonce stop a replay, and the sequence number of a batch makes a retry safe.
4. The owner starts a pairing in the app. The next unclaimed node of that kind that the gateway reports becomes the device.

Raw readings are kept 30 days. Hourly and daily rollups keep the average and the maximum of each period for 400 days, so a short spike stays visible after the raw rows are gone.
