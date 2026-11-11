# Questions and glossary

## Words from the app

The app is in Indonesian, and the API keeps a few of those words in its fields and values.

| Word | Meaning |
|:---|:---|
| WATTLINK | The name of the app, the gateway and the nodes that people see |
| UMKM | Small and medium businesses, the users of the app |
| `beranda` | The home screen, and the route `/v1/beranda` |
| `nyala`, `mati`, `siaga`, `offline`, `anomali` | The status of a device. On, off, standby, offline and anomaly |
| `baik`, `cukup`, `lemah`, `tidak-ada` | The signal of a device. Good, fair, weak and none |
| `rendah`, `sedang`, `tinggi` | The sensitivity of the anomaly rules. Low, medium and high |
| `kind` and `kindLabel` | `kind` is `plug` or `clamp`. `kindLabel` is the text for the screen and is not meant to be parsed |
| `WIB` | Western Indonesia Time, UTC+7, the zone of the default `Asia/Jakarta` |

## Questions

**Why does the app get a 429 on a route?** A route has a limit. The answer carries `retryAfterSeconds` and the header `Retry-After`. The sign in, registration, reset and pairing routes have limits, and so does the relay command, which is 60 commands an hour for a business. The reads have none. See [Errors](errors.md).

**Why does a route answer 503 with the code `busy`?** Sign in and the other routes that hash a password answer it when the password check has no free slot. Any route under `/v1` or `/device/v1` also answers it when a request ran for 10 seconds and failed, for example while the database is slow. A burst of requests that hash a password, such as sign in, registration, a change of the email and a reset, can use every slot. In both cases the request should be sent again after the number of seconds in the answer.

**Why does a new device show as offline after a short while?** A device is offline when its newest reading is older than 15 minutes, and offline wins over every other status. See [API](api.md).

**Why is there no tip yet?** A device needs at least 24 hours of data outside the work hours in the last 7 days, and it must have drawn at least 1,2 W on average in them. The home screen counts the days of data that it has collected toward 7. A tip that was applied is checked over 14 days. See [API](api.md).

**Why did no surge anomaly appear on a new device?** The rule compares a reading with the usual high power of the same hour of the day, in UTC, over the last 15 days, and it needs at least 7 such days. Before that only an overload that the node reports and a degradation can be raised.

**Why did the relay command not switch the plug?** The service cannot call a gateway. The command waits until the gateway fetches it in the answer to its next batch, and it expires after 10 minutes. A gateway that is offline never fetches it. The relay view has `gatewayOnline`, which says whether the gateway had a batch stored, or sent again after a lost answer, in the last 15 minutes. See [API](api.md#relay-control).

**Does a schedule work while the gateway has no internet?** No. The service makes the command at the set time, and a gateway that is away for more than 10 minutes never receives it.

**Why do some moments end in `Z` and others have `+07:00`?** The service stores every moment in UTC. The moments of the home screen, the running report, the devices, the relay, the list of gateways and the times of a tip are written with the offset of the business zone. The moments of the list of sessions are written in UTC, and so are the moments in the answers under `/device/v1`. A gateway sends its own times as Unix seconds. A client that wants the local clock for those converts them with the `timeZone` of `/v1/settings`.

**Why does the verification mail not arrive in a development run?** A development run sends no mail. It writes the message to the log, link included. Production needs the `SMTP_*` settings. See [Operations](operations.md).

**Can an owner delete an account?** Yes. `DELETE /v1/account` with the password deletes the business and everything under it, and it cannot be undone. See [Privacy](privacy.md#deleting-an-account).

**Can an owner remove a gateway?** Yes. `DELETE /v1/gateways/{id}` with the password removes the gateway with its devices and readings, and it cannot be undone. The hardware gets 401 and can enroll again from the start. See [API](api.md).

**Can a session be ended from the app?** Yes. The owner can list the signed in devices, end one of them, or end all the others, and signing out ends the session of that device. A session also ends after 30 days without use, after 90 days, or when the owner resets the password. See [Security](security.md).

**How long is the data kept?** Raw readings 30 days, hourly sums 400 days and anomalies 90 days. See [Privacy](privacy.md#how-long-it-stays).

**Which hardware does the firmware guide expect?** An ESP32 gateway that talks to the service over HTTPS, and plug and clamp nodes that send their readings to it over ESP-NOW. See the [firmware guide](firmware-guide.md) and the [API](api.md).
