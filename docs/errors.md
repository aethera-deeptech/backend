# Error codes

A failed response of a route under `/v1` or `/device/v1` has one JSON form, so an app reads the `code` and the HTTP status and ignores the rest of the text. The routes `/healthz` and `/readyz` have a small status body of their own, and they answer 503 when the service is not healthy. A proxy in front of the API can answer with another body or none, so an app checks the HTTP status first.

```json
{
  "code": "email_taken",
  "message": "The email has an account"
}
```

The `message` is English text for a developer and can be reworded, so an app branches on `code` and on the status and never on the message. Some answers add a field.

- `fields` names each field that failed validation, with one code for each field. The codes are `required`, `invalid`, `too_long`, `too_short`, `too_common`, `out_of_range`, `empty`, `end_before_start`, `in_the_future` and `too_old`.
- `retryAfterSeconds` is on every 429 answer and on the 503 answer `busy`, and the same number is in the `Retry-After` header.
- `expectedSeq` is on the answer `sequence_gap` and holds the sequence number that the service wants next.

An error that the service did not plan is answered as a plain 500 with the code `internal` and no detail. The routes that are listed in [openapi.yaml](../openapi.yaml) name the codes that each of them can answer.

## Any route

The codes `unauthorized`, `csrf_header_missing` and `origin_not_allowed` apply to the routes under `/v1` only, because the device routes under `/device/v1` take no session and no CSRF header.

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `unauthorized` | 401 | The request has no session, or the session ended | Send the person to sign in |
| `csrf_header_missing` | 403 | A request that changes data has no `X-Requested-With: aethera` header, or the value is another one | Send that header on every request of the app |
| `origin_not_allowed` | 403 | The `Origin` header is not the origin of the app | A deployment setting, so tell the owner of the deployment |
| `not_found` | 404 | The id does not exist or belongs to another business, and the two cannot be told apart. A path with no route gives the same answer | Show that the item is gone |
| `method_not_allowed` | 405 | The route does not take this method | A bug of the app |
| `request_timeout` | 408 | The request took too long to arrive | Send it again |
| `payload_too_large` | 413 | The body is over the limit of the route | Send less |
| `invalid_json` | 400 | The body is empty, is not the JSON that the route expects or holds more than one value | A bug of the app |
| `validation_failed` | 422 | A field is not valid, and `fields` says which | Show the message of each field next to it |
| `bad_request` | 400 or another status that the HTTP server chose | The HTTP server refused the request before a route saw it, and the code does not say more | Check the HTTP status |
| `rate_limited` | 429 | A limit of the route was passed | Wait `retryAfterSeconds` and send again |
| `busy` | 503 | The password check has no free slot, and the answer comes with a short retry time | Send the request again after a moment |
| `internal` | 500 | The service failed and has logged it | Show a general failure, and send again later |

## Account

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `email_taken` | 409 | The email already has an account. It is also the answer to a change to an email that another account uses | Ask for another email, or send the person to sign in |
| `invalid_credentials` | 401 | The email or the password is wrong, with the same answer for both. It also answers a wrong password on the change of the email | Show one sign in error |
| `email_not_verified` | 403 | The password is right but the email is not verified yet | Offer to send the verification mail again |
| `token_invalid_or_expired` | 410 | The link of a mail is unknown, used or expired | Ask the person to request a new mail |
| `resend_too_soon` | 429 | A second verification or reset mail for the same address within a minute | Wait `retryAfterSeconds` |
| `already_verified` | 409 | The email is verified, so it cannot be changed here | Show that the email is fixed |

A change of the email to the address that the account already has answers 422 `validation_failed` with the field `newEmail` set to `invalid`, after the password is checked.

## Gateways, devices and pairing

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `claim_code_unknown` | 404 | The claim code is unknown, used or expired | Ask the person to read the code from the gateway again |
| `too_many_gateways` | 409 | The business has the most gateways that it can have | Show the limit |
| `gateway_claimed_elsewhere` | 409 | The gateway belongs to another business | Show that the gateway belongs to another business |
| `key_in_use` | 409 | A value that must be unique is taken already. It is the key of another gateway in most cases, and the hardware id when two claims for the same gateway ran at the same time | Send the claim again. When the key is the cause, the gateway makes a new key and enrolls again. When the same claim now answers `gateway_claimed_elsewhere`, the gateway belongs to another business |
| `no_gateway` | 409 | The business has no gateway yet, so there is nothing to pair a device to | Send the person to claim a gateway first |
| `pairing_running` | 409 | A search for a device is already running | Show the search that runs or wait for its end |

## Gateway protocol

These codes are answers to the device routes under `/device/v1`. The [firmware guide](firmware-guide.md) says what a gateway does for each one.

| Code | Status | Meaning |
|:---|:---|:---|
| `invalid_gateway_key` | 401 | The key in the bearer header is not valid |
| `enrollment_expired` | 410 | The claim code expired, so the gateway enrolls again |
| `unsupported_encoding` | 415 | The body of a batch is compressed, which the service does not accept |
| `payload_too_large` | 413 | A batch is over 256 KiB or holds more than 5000 samples |
| `sequence_gap` | 409 | The batch skips a sequence number, and `expectedSeq` holds the one the service wants |

## Rooms, tips and relay

| Code | Status | Meaning | What the app does |
|:---|:---|:---|:---|
| `too_many_rooms` | 409 | The business has the most rooms that it can have | Show the limit |
| `room_exists` | 409 | A room with this name exists | Ask for another name |
| `invalid_transition` | 409 | The tip is not in a state that allows this decision | Reload the tip and show its state |
| `no_relay` | 409 | Only a plug has a relay | Hide the switch for a clamp |
