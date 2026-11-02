# Firmware guide

This guide is for the person who writes the firmware of the WATTLINK gateway (the ESP32 hub). It says what the gateway sends to the service, what the service answers, and what the gateway does with each answer. The exact schemas are in `openapi.yaml`, and `docs/api.md` explains the rules behind them. Every route of the gateway is under `/device/v1` and uses HTTPS and JSON.

The gateway builds against this REST API.

## The life of a gateway

1. The gateway makes a random key once and keeps it in flash. The service accepts a key of 32 to 128 characters, and the recommended key is 43 characters of base64url text, for example 32 random bytes written as base64url with no padding.
2. It calls `POST /device/v1/enroll` and shows the claim code to the owner.
3. It polls `GET /device/v1/enroll/{pollId}` until the owner has typed the code in the app.
4. From then on it sends `POST /device/v1/readings` with its key as a bearer token, for ever.

A gateway that loses its flash makes a new key and enrolls again, and the owner claims the new code. A gateway keeps its sequence number on the service side, so the first batch after a new enroll with the same hardware id can answer 409 with a higher `expectedSeq`, and the gateway continues from that number (see the answer to a batch below).

## 1. Enroll

```json
POST /device/v1/enroll
{"hardwareId": "ESP32-A1B2C3D4E5F6", "firmware": "0.3.1", "keyHash": "<64 lower case hex characters>"}
```

- `hardwareId` has 6 to 64 characters from `A-Z a-z 0-9 : _ -`. Use the MAC address or the chip id. One hardware id can ask for 10 codes in an hour, and it belongs to one business. A hardware id that another business claimed already answers 409 `gateway_claimed_elsewhere` when the owner claims it, and a changed hardware id looks like a new gateway. A business can have 10 gateways.
- `firmware` is optional, at most 32 characters, with no line break or null character.
- `keyHash` is the SHA-256 of the 43 characters of the key text, not of the decoded bytes, written in lower case hex. The service never sees the key.
- The answer 201 holds `claimCode` (8 characters, no letters that look alike), `pollId` and `expiresInSeconds` (600). Show the code on the display or send it to the phone.
- A network can ask for 20 codes in an hour, and the answer 429 says how long to wait in the `Retry-After` header and in `retryAfterSeconds`.

## 2. Poll

`GET /device/v1/enroll/{pollId}` answers 202 while the code waits. A network has a limit of 300 polls in 15 minutes, shared by every gateway on it, so poll every 5 seconds or slower. The answer 200 says `{"gatewayId": "..."}`, and the gateway now belongs to a business. The enrollment is gone after that answer, so the next poll answers 404. A lost 200 turns into a 404 on the retry, and then the gateway sends one batch of readings to see whether its key works. The answer 410 and an answer 404 before any 200 both mean that the code expired, because the service deletes expired codes when anyone enrolls, so enroll again.

## 3. Send readings

```json
POST /device/v1/readings
Authorization: Bearer <the 43 characters of the key>
{
  "seq": 41,
  "sentAt": 1793618400,
  "nodes": [
    {"nodeId": "PLUG-0001", "kind": "plug", "hops": 1,
     "samples": [{"t": 1793618390, "v": 221.4, "i": 0.9, "p": 198.5, "e": 12.431, "f": 50.0, "pf": 0.91}]}
  ],
  "results": []
}
```

### The batch

- `seq` is the number of the batch. The first batch of a new gateway is 1, and every new batch is the one before plus one. The service stores a batch once, so a gateway that did not see the answer sends the same batch again with the same number and nothing is stored twice.
- `sentAt` is the Unix time on the clock of the gateway, read when the batch is sent from the same clock that made the `t` of the samples. It must be 1700000000 or more (14 November 2023), or the answer is 422, so a gateway whose clock still starts near 1970 has to set it first, from a time server or from `serverTime` of an earlier answer. The service moves every time stamp of the batch by the difference between `sentAt` and its own clock when the two differ by more than 5 seconds, so a clock that is set but wrong still lands the readings at the right time as long as it runs at the right speed. A clock with the right time is better.
- `nodes` has 1 to 16 nodes, and a gateway has at most 64 different nodes. A batch has at most 1000 samples for each node and 5000 in total, and the body is at most 256 KiB and must not be compressed (the answer is 415).
- A gateway has a limit of 600 batches in an hour, which is one every 6 seconds. The service counts every batch that it receives, also a retry and one that it refuses, before it looks at the content. A gateway that sends a backlog after an outage can reach the limit, so send the backlog in a few large batches and not in many small ones. As advice, send a batch every 10 to 60 seconds. A batch can hold up to 24 hours of samples, so a gateway that lost its Wi-Fi sends what it kept when it comes back.
- A batch must hold at least one node, even when the gateway only wants to ask for commands. Put the newest sample of one node in it.

### The node

- `nodeId` has 6 to 32 characters from `A-Z a-z 0-9 : _ -` and is unique inside the gateway. `kind` is `plug` or `clamp`. `hops` is how many nodes relayed the packet, 0 to 3.
- A node with no sample, or with more than 1000, is dropped with its samples and counted in `rejected`, and it is not an error. A node that no device claimed yet is kept, and the owner can claim it with a pairing in the app. The next unclaimed node of that kind that the gateway reports during a pairing becomes the device.

### The sample

| Field | Meaning | Limit |
| :--- | :--- | :--- |
| `t` | Unix seconds of the sample, required | not older than 24 hours, not more than a minute ahead |
| `p` | Power in watts, required | 0 to 60000 |
| `v` | Volts | 0 to 500 |
| `i` | Amperes | 0 to 200 |
| `e` | Energy counter of the node in kWh since it was made | 0 or more, and it should not be reset |
| `f` | Hertz | 0 to 100 |
| `pf` | Power factor | 0 to 1 |
| `a` | Condition that the gateway classified | 0 normal, 1 standby, 2 motor degradation, 3 overload |
| `r` | State of the relay of a plug | 1 on, 0 off |

- Only `t` and `p` are required. Leave out a field that the node cannot measure and do not send 0 for it.
- A sample that is out of range, older than a day or from the future is dropped and counted in `rejected` in the answer, so one bad sample never blocks the gateway. A node that is built wrong, that repeats in the batch or that goes over the 64 nodes is dropped with its samples.
- Send `e` when the node has a counter. The service uses it for the energy of an hour when the gap between two samples is at most an hour and the step is plausible. A counter that falls or jumps is ignored for that gap. Without a counter the energy is the power times the time, and a gap counts at most 5 minutes of power, so send a sample at least every 5 minutes.
- Send `pf` for a motor. The service raises a degradation when a batch holds at least 3 samples of 150 W or more that carry `pf`, and at least 80 percent of those are under 0,55. Only samples of the last 2 hours count. A batch of one or two samples never raises it, so send the samples of one node together.
- Send `a` 2 or 3 when the gateway decided so on its own, for example while the internet was down. The service keeps it and decides on its own too.

### The answer

```json
{"ackSeq": 41, "serverTime": "2026-11-03T08:01:02Z", "accepted": 1, "rejected": 0, "commands": []}
```

- `ackSeq` equals `seq` when the batch is stored. The gateway deletes the batches up to this number from its buffer.
- `serverTime` is the clock of the service. Use it to set the clock of the gateway when the clock is unset or far off.
- `commands` holds the relay commands that wait for this gateway, see the next section.
- The answer 409 with `expectedSeq` means that the batch skips a number or that its number is lower than the one that the service stored last. A gateway that lost its counter (a reset or a new key) learns from this where to continue. Send the batch again with `seq` equal to `expectedSeq`, and renumber the batches that are still in the buffer.
- The answer 401 means that the service does not know the key. A sensible policy is to stop and enroll again. The answer 413 means that the batch is too large, so send fewer samples. The answer 422 names the fields that are not valid. The answer 400 `invalid_json` means that the body is not JSON, is empty or holds a field that the service does not know, so do not add fields of your own. The answer 429 names the wait in `retryAfterSeconds`, so wait that many seconds.
- A network error or a 5xx answer is a retry of the same batch with the same `seq`, after a pause that grows from 5 seconds to 5 minutes.

## 4. Relay commands

A plug has a relay. The service cannot call the gateway, because the gateway sits behind a router and only calls out. So the owner's command waits in the service, and the gateway gets it in the answer to its next batch.

1. The answer holds `commands`, for example `[{"id": "6f1c...", "nodeId": "PLUG-0001", "action": "off", "expiresAt": "2026-11-03T08:11:02Z"}]`.
2. The gateway switches the relay of that node. It skips a command whose `expiresAt` has passed, because a plug is never switched on an old order. `expiresAt` is in the time of the service, so compare it with `serverTime` of the answer and not with an unset clock of the gateway.
3. The gateway reports the result in the next batch, with the same id. `ok` is true when the relay switched and false when it could not.

```json
"results": [{"id": "6f1c...", "ok": true}]
```

- A command comes again in every answer until the gateway reports a result for it, until it expires after 10 minutes or until the owner sends a newer command for the same plug, so the gateway runs each id once and keeps the ids that it already ran. After a restart, a command that comes again and was already run only gets its result sent again.
- `action` is `on` or `off`, never a toggle, so running a command twice does no harm.
- A batch can carry at most 64 results. A result for a command that ended or that belongs to another gateway is ignored.
- Send `r` in the samples of a plug so the app shows the real state, also after someone switched the relay by hand.
- A schedule of the owner makes the same kind of command. The gateway does not need to know the schedule.

## What the gateway keeps

- The key, in flash, never printed to a log.
- The next `seq` and the buffer of batches that are not acknowledged yet. A gateway with no buffer loses the readings of the time without internet, and a gateway with a small buffer should drop the oldest samples first.
- The ids of the commands that it ran, for as long as a command can live, which is 10 minutes.

## Quick test with curl

```sh
curl -sS https://aethera-api.syafahadyan.com/device/v1/readings \
  -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d "{\"seq\":1,\"sentAt\":$(date +%s),\"nodes\":[{\"nodeId\":\"PLUG-0001\",\"kind\":\"plug\",\"samples\":[{\"t\":$(date +%s),\"p\":42.5}]}]}"
```

Use a throwaway business for a test and not the demo account of the product.
