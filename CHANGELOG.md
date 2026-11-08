# Changelog

## [0.9.0](https://github.com/aethera-deeptech/backend/compare/v0.8.0...v0.9.0) (2026-11-08)


### ⚠ BREAKING CHANGES

* **device:** send null for a 15 minute point of the power pattern with no reading
* **device:** send null for a 15 minute point of the power pattern with no reading

### Features

* **device:** send null for a 15 minute point of the power pattern with no reading ([415cac8](https://github.com/aethera-deeptech/backend/commit/415cac8caea1a353591bbac1a5fa86e6f0b9294e))
* **device:** send null for a 15 minute point of the power pattern with no reading ([6babefc](https://github.com/aethera-deeptech/backend/commit/6babefcba3da9e319e8f33f15b03b87eab41db54))

## [0.8.0](https://github.com/aethera-deeptech/backend/compare/v0.7.0...v0.8.0) (2026-11-08)


### Features

* **device:** add the id of the running search to the pairing_running answer ([584150c](https://github.com/aethera-deeptech/backend/commit/584150c75aa46cedd7bd35202dbaf601dc37d947))
* **device:** add the id of the running search to the pairing_running answer ([72deab2](https://github.com/aethera-deeptech/backend/commit/72deab2106285bc92faf9a361a34c15f025db438))


### Bug Fixes

* **deps:** update the indirect modules brotli and compress ([6b7fd6a](https://github.com/aethera-deeptech/backend/commit/6b7fd6ae683e4816ab0873c0b973d25e42d0a025))
* **deps:** update the indirect modules brotli to 1.2.7 and compress to 1.20.2 ([5413bab](https://github.com/aethera-deeptech/backend/commit/5413bab84fb984822ecea709c7dc384fa29a2219))
* **device:** release the transaction before the lookup of the running search and say that pairingId is optional ([b352d1b](https://github.com/aethera-deeptech/backend/commit/b352d1bbd62ad02bf677936d72c6e6eaec138586))

## [0.7.0](https://github.com/aethera-deeptech/backend/compare/v0.6.0...v0.7.0) (2026-11-03)


### Features

* **device:** let the owner start the energy total of a device again ([ce61785](https://github.com/aethera-deeptech/backend/commit/ce61785f093fbc0baac2b2aa7ff9562caea0b781))
* **device:** let the owner start the energy total of a device again with a reset time ([ab3fbc3](https://github.com/aethera-deeptech/backend/commit/ab3fbc3fef56752f18ea2e09a65e4e5e497d2837))
* **relay:** show that a plug cut itself off after an overload ([8dbf9bc](https://github.com/aethera-deeptech/backend/commit/8dbf9bc2caeb8bfb95cf570d02a326048d5ce635))
* **relay:** show that a plug cut itself off after an overload in the relay view ([a8d908a](https://github.com/aethera-deeptech/backend/commit/a8d908aaf43ec3072e67578f799ac168b6fcd4c9))


### Bug Fixes

* **device:** quote the resetAt description, truncate the reset time to microseconds and test the hour of the reset ([1417d81](https://github.com/aethera-deeptech/backend/commit/1417d810366ffbba683bbbb15db488193cd39e11))
* **relay:** compare the overload with the fetch of the command and document the limits of the cause ([5915a23](https://github.com/aethera-deeptech/backend/commit/5915a23acb8b2e3bb22fbf3d46f874c27f396c68))
* **relay:** move the fetch time in the test fixture and say that the field is an indication that ends after 24 hours ([b2fc22f](https://github.com/aethera-deeptech/backend/commit/b2fc22ffded8a325f085059b99a46c7e1ea9daf9))

## [0.6.0](https://github.com/aethera-deeptech/backend/compare/v0.5.0...v0.6.0) (2026-11-03)


### Features

* **auth:** refuse a password from the list of common passwords with the code too_common ([0ffb6e6](https://github.com/aethera-deeptech/backend/commit/0ffb6e6c5580383c7527db5be501a76a28e28352))
* **auth:** refuse common passwords with the code too_common ([35c8ce7](https://github.com/aethera-deeptech/backend/commit/35c8ce79ca3f8090bb42f125ec34daa23fb76bdb))

## [0.5.0](https://github.com/aethera-deeptech/backend/compare/v0.4.1...v0.5.0) (2026-11-03)


### Features

* **device:** add the kind of a device to the list and the detail ([cf29c7a](https://github.com/aethera-deeptech/backend/commit/cf29c7a335e962868e1d211334d11480db7e0808))
* **device:** add the kind of a device to the list and the detail ([8be49f8](https://github.com/aethera-deeptech/backend/commit/8be49f815666cceabdb1101556bbc90ddcedd447))
* **relay:** add gatewayOnline to the relay view ([04e59a8](https://github.com/aethera-deeptech/backend/commit/04e59a8895324a6e4c314bffd7b64b49557b41fd))
* **relay:** add gatewayOnline to the relay view ([1be26eb](https://github.com/aethera-deeptech/backend/commit/1be26eb954060359fbe7d33f9fd5ab1c0725411c))


### Bug Fixes

* **gateway:** lower the load floor of the degradation rule from 150 W to 35 W ([f7ac099](https://github.com/aethera-deeptech/backend/commit/f7ac0992100f5bff04f377ee1ba3470c8adf41a8))
* **gateway:** lower the load floor of the degradation rule from 150 W to 35 W ([fafcc2b](https://github.com/aethera-deeptech/backend/commit/fafcc2b02d3eeb66b024f56c24003eeed46d4a68))
* **gateway:** tell firmware to send pf for motors only and test the 35 W boundary ([782717a](https://github.com/aethera-deeptech/backend/commit/782717aa7284b47260d322b3943bdc53b081fc92))
* **relay:** count only the commands that were queued against the hourly limit ([7e67c7a](https://github.com/aethera-deeptech/backend/commit/7e67c7aeb3f5a312541083807d9941e8b41a9895))
* **relay:** count only the commands that were queued against the hourly limit ([3a7365f](https://github.com/aethera-deeptech/backend/commit/3a7365fb06e7d9795450ea99912e13092c826a71))

## [0.4.1](https://github.com/aethera-deeptech/backend/compare/v0.4.0...v0.4.1) (2026-11-03)


### Bug Fixes

* **api:** log the route pattern and the error type instead of the called path and the error text ([aad82ed](https://github.com/aethera-deeptech/backend/commit/aad82ed16de7c747b30f71a9835487b69f0a6d30))
* **api:** log the route pattern and the error type instead of the called path and the error text ([7ec11a0](https://github.com/aethera-deeptech/backend/commit/7ec11a0eaeb00a6c3ea67d216e8e7a4d08dae75d))
* **api:** log unmatched requests by a fixed label and follow wrapped errors safely ([af5cd6a](https://github.com/aethera-deeptech/backend/commit/af5cd6af48911e02e435290ad9163e7d65c901c6))
* **api:** send the same-origin resource policy header and drop the ZAP ignore for it ([6b9e657](https://github.com/aethera-deeptech/backend/commit/6b9e6578cb9806f96a6f044e2eb51aba9def2444))
* **api:** send the same-origin resource policy header and drop the ZAP ignore for it ([3b64181](https://github.com/aethera-deeptech/backend/commit/3b64181cbea96318eedabe09e88e96ac61466841))

## [0.4.0](https://github.com/aethera-deeptech/backend/compare/v0.3.0...v0.4.0) (2026-11-02)


### Features

* **monitoring:** add Grafana Alloy config, dashboards, alert rules and docs ([c3e6143](https://github.com/aethera-deeptech/backend/commit/c3e6143f2a694b66faf0d8e06141d95cf68b21d6))
* **monitoring:** add Grafana Alloy config, dashboards, alert rules and docs ([8ee2712](https://github.com/aethera-deeptech/backend/commit/8ee2712da7d0c7a5e87b0f4b60efd367f75bdd68))


### Bug Fixes

* **auth:** refuse a change to the same address and list both 409 codes of change-email in the contract ([d241ca6](https://github.com/aethera-deeptech/backend/commit/d241ca6baf73cb7552879645986348a95cb06dc8))
* **auth:** refuse a change to the same address and list both 409 codes of change-email in the contract ([e59db1e](https://github.com/aethera-deeptech/backend/commit/e59db1e0f9f7a1b450d2c52c1cfbedb93e6a8423))
* **monitoring:** give Alloy its own env file and fix the review findings ([ac83dbf](https://github.com/aethera-deeptech/backend/commit/ac83dbfe9e68d8b0996ec63b47b9271b5c857d57))
* **monitoring:** reorder the setup steps and fix two small review findings ([4badd50](https://github.com/aethera-deeptech/backend/commit/4badd50fc5335bf5e452d3f663e590cc65b602c0))

## [0.3.0](https://github.com/aethera-deeptech/backend/compare/v0.2.1...v0.3.0) (2026-11-02)


### ⚠ BREAKING CHANGES

* **report:** add the report and the PDF of an ISO week next to the monthly ones

### Features

* **anomaly:** raise a degradation for a loaded motor with a poor power factor and show the kind of the anomaly ([e303401](https://github.com/aethera-deeptech/backend/commit/e30340138c9a8e61560e182836fe7f19bf663dd2))
* **anomaly:** raise a degradation for a loaded motor with a poor power factor and show the kind of the anomaly ([d932ace](https://github.com/aethera-deeptech/backend/commit/d932ace70671347c47b7ad8de075a5364daab3da))
* **health:** report the database, the pool and the uptime in /healthz and answer 503 when the database is down ([c726a3c](https://github.com/aethera-deeptech/backend/commit/c726a3c20655418d6dcb4a4cee5e9dd558b2201b))
* **health:** report the database, the pool and the uptime in /healthz and answer 503 when the database is down ([7e2586a](https://github.com/aethera-deeptech/backend/commit/7e2586a1616fe2c69ed31b8c70c059a936e261fb))
* **relay:** switch the relay of a plug on a command or a schedule of the owner through the answer to the gateway ([c3ed117](https://github.com/aethera-deeptech/backend/commit/c3ed117beb3435250fe3d2c139026db0e4c8feb8))
* **relay:** switch the relay of a plug on a command or a schedule of the owner through the answer to the gateway ([eec009a](https://github.com/aethera-deeptech/backend/commit/eec009afb072c9efff31b4911d17239ccad437a8))
* **report:** add the report and the PDF of an ISO week next to the monthly ones ([2a241b7](https://github.com/aethera-deeptech/backend/commit/2a241b7ce9e441d25bbbafc0200662e563e654f2))


### Bug Fixes

* **anomaly:** quote the power factor description, keep no power factor for a reported degradation and reuse round2 ([8ff16c4](https://github.com/aethera-deeptech/backend/commit/8ff16c4a356a90401710b6a32c36378228282843))
* **anomaly:** use plain constraints in the degradation migration and ignore the squawk rules for the small table ([0c3eec6](https://github.com/aethera-deeptech/backend/commit/0c3eec631950d5d345ff42204ef2c90b0ddfe894))
* **relay:** pass the parsed id to the queries and keep a newer sample over a command result ([26ee9de](https://github.com/aethera-deeptech/backend/commit/26ee9de128fbeb2a33adafdfdb8e973a6fa393f3))
* **relay:** use the canonical device id in every query of a route ([171b273](https://github.com/aethera-deeptech/backend/commit/171b27358686b23ff42a336ad33786144887c0e7))
* **report:** name both years of a week at the new year and keep the period pattern for months and weeks ([0c26fd3](https://github.com/aethera-deeptech/backend/commit/0c26fd3900f71d6d8ad85acea6eaa5176818da15))

## [0.2.1](https://github.com/aethera-deeptech/backend/compare/v0.2.0...v0.2.1) (2026-11-02)


### Bug Fixes

* **device:** name the plug and the clamp WATTLINK Plug and WATTLINK Clamp in the kind label ([7919ff0](https://github.com/aethera-deeptech/backend/commit/7919ff00b5554404d10d3dc92f0e9789d8183f80))
* **device:** name the plug and the clamp WATTLINK Plug and WATTLINK Clamp in the kind label ([814d191](https://github.com/aethera-deeptech/backend/commit/814d1911aff9423af3a52b5b4f8bbfc0ed92732c))

## [0.2.0](https://github.com/aethera-deeptech/backend/compare/v0.1.0...v0.2.0) (2026-11-02)


### Features

* **api:** add the error type, the strict JSON reader and the middleware ([7d60553](https://github.com/aethera-deeptech/backend/commit/7d605534890e45f66740d3c5fd3599845d383952))
* **api:** report a 5xx and a panic and add the Sentry settings ([c032947](https://github.com/aethera-deeptech/backend/commit/c032947250b9bbb67d10292c9b541176f2697599))
* **api:** serve the metrics on METRICS_ADDRESS and measure every request ([bba34c6](https://github.com/aethera-deeptech/backend/commit/bba34c6c789688a363852008b5451c9df2f2c073))
* **app:** mount the production routes ([ec292f9](https://github.com/aethera-deeptech/backend/commit/ec292f948f2280846522879d9f2ab7b24bfe2c8f))
* **app:** mount the settings and rooms routes ([36cf86c](https://github.com/aethera-deeptech/backend/commit/36cf86c5c4a0236bdcf67c9a2f3725cefd9ea118))
* **auth:** add sign up, email verification, sign in, sessions and password reset ([83bde72](https://github.com/aethera-deeptech/backend/commit/83bde72302422ee9a36c21d94157d46c376d95ee))
* **auth:** add sign up, email verification, sign in, sessions and password reset ([47af9c6](https://github.com/aethera-deeptech/backend/commit/47af9c695c4d71c5cd9a31eec9bc3065e833ed65))
* **business:** add the settings and rooms routes with the room table ([9163595](https://github.com/aethera-deeptech/backend/commit/916359519bf24845035dacbca2728ad1e1944495))
* **business:** add the settings and the rooms ([de46a03](https://github.com/aethera-deeptech/backend/commit/de46a03ed3c5789ca61c3f9739945935eebc726a))
* **config:** add the app origin, proxy and mail server settings ([adc2ce2](https://github.com/aethera-deeptech/backend/commit/adc2ce29420374db8ee8d182fdc360b2af8a0587))
* **config:** add the database settings and check the env example in a test ([9908be8](https://github.com/aethera-deeptech/backend/commit/9908be88fa8c4fb564dc77fda6233b1efaca7105))
* **database:** add the pool, the embedded migrations and the first migration for businesses ([a8d429a](https://github.com/aethera-deeptech/backend/commit/a8d429afe20de3cf722e3982b6c6911c228c9062))
* **database:** add the PostgreSQL pool, the migrations and a readiness route ([b35ea8e](https://github.com/aethera-deeptech/backend/commit/b35ea8e04956f7c295df876522100da197d9c26f))
* **database:** add the tables for users, sessions, email tokens and rate limits ([65d64ba](https://github.com/aethera-deeptech/backend/commit/65d64bad72843b1e12d22fc51fa513201630bf70))
* **device:** add the pairing of a new device and mount the gateway routes ([92ed0e5](https://github.com/aethera-deeptech/backend/commit/92ed0e5c5d8c7248abea5c5388cef199fcde8fcf))
* **device:** classify the status and the signal of a device ([ffad571](https://github.com/aethera-deeptech/backend/commit/ffad571459600175eb346f455d3e27f0e1b7fec2))
* **device:** list the devices, show one with its power pattern and rename it ([5dba4d8](https://github.com/aethera-deeptech/backend/commit/5dba4d8b8c1783bcfbd491d9c9fbaa740c0ed9da))
* **device:** list the devices, show one, rename it and raise anomalies ([2e9f4d7](https://github.com/aethera-deeptech/backend/commit/2e9f4d71a8b4a22d65fea41874fe63ac22cbf304))
* **errorreport:** add the scrubbed error reports to Sentry with a cap for each minute ([1022a7e](https://github.com/aethera-deeptech/backend/commit/1022a7e46b2e90ce84b82dd57bf5c4e2cdd22269))
* **errorreport:** send a 5xx and a panic to Sentry without secrets ([53be9e5](https://github.com/aethera-deeptech/backend/commit/53be9e573a1f3ef57d4ee42cad099d2e06641686))
* **gateway:** add the routes of the app and of the gateway ([61c7e54](https://github.com/aethera-deeptech/backend/commit/61c7e546d0024872521129ffefe18dcc5975351c))
* **gateway:** add the tables, the enrollment and the batch intake of the gateway ([7e141e6](https://github.com/aethera-deeptech/backend/commit/7e141e62c9d8de966e8734411a6d7802321b62f1))
* **gateway:** enroll a gateway, take its readings and pair a device ([021998d](https://github.com/aethera-deeptech/backend/commit/021998df5e2b72041ee1e877bbf5e59185dc51ad))
* **gateway:** keep hourly sums of the readings and purge the old ones ([d3302a3](https://github.com/aethera-deeptech/backend/commit/d3302a373a3cd457830f0cea9a1f52eb0212f434))
* **gateway:** keep hourly sums with the highest power and the energy of each hour ([904254c](https://github.com/aethera-deeptech/backend/commit/904254ced0078914bd399dfba3b29db01dc51f94))
* **gateway:** raise an anomaly for a surge above the usual power or an overload of the node ([34b0f99](https://github.com/aethera-deeptech/backend/commit/34b0f99a70a1235dea2131e59fb111dd22e9c8e0))
* **home:** build the home screen with the statuses, the tips, the top devices and the bill ([df2b376](https://github.com/aethera-deeptech/backend/commit/df2b3769280acec6be84118ef238fe1ffe0d758d))
* **janitor:** purge old sessions, tokens, limit events and unverified accounts ([df11092](https://github.com/aethera-deeptech/backend/commit/df11092b149e4a8373d81917ba988539ad5ca299))
* **mail:** send emails over SMTP through a background queue ([db992d8](https://github.com/aethera-deeptech/backend/commit/db992d8bc171d19df828d103ec9c1f7e59f089c9))
* **metrics:** count the requests and the pool by route pattern on a listener of its own ([caa4107](https://github.com/aethera-deeptech/backend/commit/caa410791cf1aaf9d96ca3ab337f7c1d797b30b0))
* **metrics:** serve Prometheus metrics on a private listener ([1a5bfca](https://github.com/aethera-deeptech/backend/commit/1a5bfcacecd67511cde24c91e77ba596e10e2e85))
* **production:** add the daily production records and their routes ([b773ec7](https://github.com/aethera-deeptech/backend/commit/b773ec733547ed64c0cc62db62b87b67675b1a75))
* **production:** record the production of each day ([e3e7bc4](https://github.com/aethera-deeptech/backend/commit/e3e7bc4f2dbf5e16587bd0ff08a177e7132acf81))
* **report:** build the monthly report and draw it as a PDF ([e896711](https://github.com/aethera-deeptech/backend/commit/e8967111d858c5d0acf89e71272d64af7c70e15c))
* **report:** build the report of a month with the three sums, the top devices and the bill ([c90ba58](https://github.com/aethera-deeptech/backend/commit/c90ba587fc3d8ea1f7ce676135fed6a30b9f6b77))
* **report:** draw the report as one A4 page of PDF ([c9d5800](https://github.com/aethera-deeptech/backend/commit/c9d58001f88dd0825582ca13d64e96c99a38f6d2))
* **server:** add CORS, the CSRF check and the trusted proxy and mount the account routes ([6aec0db](https://github.com/aethera-deeptech/backend/commit/6aec0db3b9d465dfdd9268887010a6976602ba00))
* **server:** migrate and open the database at start and add the readiness route ([8461ee8](https://github.com/aethera-deeptech/backend/commit/8461ee8b99cab7029a5e175ded88c4f4530ad3f8))
* **tips:** add the rules and the Indonesian texts of a tip ([1fcb83c](https://github.com/aethera-deeptech/backend/commit/1fcb83c60de8cb70adcf2c213eb69634695015ca))
* **tips:** list the tips, decide on them and show the home screen ([8fa7f4d](https://github.com/aethera-deeptech/backend/commit/8fa7f4dd5e8f994d6d6db59695ff784244af8215))
* **tips:** list the tips, decide on them and verify the applied ones ([c7a7621](https://github.com/aethera-deeptech/backend/commit/c7a76214fdf4c06611b46b4ebb2bce7c83b0106b))


### Bug Fixes

* **api:** count an IPv6 client by its prefix, cap account bodies and log panics ([f0c2283](https://github.com/aethera-deeptech/backend/commit/f0c228334e410f84f38147bd85dfa4f8230ebc4d))
* **auth:** bind email tokens to their address and make the email change atomic ([a2e808f](https://github.com/aethera-deeptech/backend/commit/a2e808f37625fff1f75df2ad26b9372a6b4680cf))
* **auth:** lock the user before the token and refuse a wildcard origin ([4346abf](https://github.com/aethera-deeptech/backend/commit/4346abf8111572fdd063c333e8ffb3ec0df22ece))
* **auth:** make the dummy hash at start and lock the user when a token is saved ([3b3d71b](https://github.com/aethera-deeptech/backend/commit/3b3d71b012f96f3a3894b0dd9c7e124fa6129057))
* **auth:** run the session check before the route handler and satisfy the linters ([255c9ba](https://github.com/aethera-deeptech/backend/commit/255c9ba910e81b80c409991794e15615e04127c2))
* **business:** check the zone against the database, lock the room cap and return the saved settings ([87d15b3](https://github.com/aethera-deeptech/backend/commit/87d15b3f6f5a8d5c8cdc444373b452f68e57f4e9))
* **business:** lock the business before the room count so the count sees the newest room ([f7b8a3a](https://github.com/aethera-deeptech/backend/commit/f7b8a3a987712933b4203fa32d72b5af5286148b))
* **business:** refuse hidden format characters in names ([d2a63d1](https://github.com/aethera-deeptech/backend/commit/d2a63d1f5f2269351fc8f7b62c471ffb41cebeca))
* **business:** run the session check before the route handlers ([b30b4d5](https://github.com/aethera-deeptech/backend/commit/b30b4d5c4ea5ed11bff932db88236dd225f39c16))
* **business:** show a placeholder name instead of null ([dc6f5a5](https://github.com/aethera-deeptech/backend/commit/dc6f5a5a213871f60b4ca5dd28198adacafd3fce))
* **config:** classify the parsed address when it checks a mapped range ([ce9f30e](https://github.com/aethera-deeptech/backend/commit/ce9f30e8a04639be3c260be3d2f7cfed036798a4))
* **config:** refuse a mapped IPv4 range shorter than 96 bits in the proxy list ([ce2e482](https://github.com/aethera-deeptech/backend/commit/ce2e482f68d136c40bc0be90bad7db96e579fc2a))
* **config:** write the app origin like a browser and add a trusted proxy list ([478c753](https://github.com/aethera-deeptech/backend/commit/478c75381b4298ca5d7e3de4c45bf0c23dc3fb11))
* **database:** bound each ping and allow the migration file names in ls-lint ([a231dc7](https://github.com/aethera-deeptech/backend/commit/a231dc701d8fddfab03e4c4189b658f55aa0fc23))
* **database:** leave the lock timeout to the migrations that change a table ([662b5f0](https://github.com/aethera-deeptech/backend/commit/662b5f0967c8964c8fe3f4738e1d02e69fd6d700))
* **database:** open before migrating, bound the waits and skip when the database is ahead ([73b60d1](https://github.com/aethera-deeptech/backend/commit/73b60d1dc26d828e4c8a50df098dbed888d18986))
* **device:** end the history of the expected band at the hour before the chart ([36c1e43](https://github.com/aethera-deeptech/backend/commit/36c1e431822dfa877888091e5b3c8100b02c333d))
* **device:** need a week of history, debounce each kind, and keep the intake safe from a failed anomaly check ([6241adc](https://github.com/aethera-deeptech/backend/commit/6241adcad36f82a02e9c08ced944d8217cb6ea85))
* **errorreport:** cap before the capture, guard the reporter, report a failed start and scrub addresses ([a658048](https://github.com/aethera-deeptech/backend/commit/a658048fd1e433ab2125b6e58bdd8b57da5726da))
* **errorreport:** log a fixed text for a bad DSN and scrub compressed IPv6 addresses ([be24a22](https://github.com/aethera-deeptech/backend/commit/be24a2241cca2af7c995e40c8945ea7a30fe4e72))
* **errorreport:** scrub a long secret before an IPv6 address ([9dd6f87](https://github.com/aethera-deeptech/backend/commit/9dd6f8720d3268ec7457b116ae8b4b4b0ed6017c))
* **gateway:** bound the nodes, drop a bad node and not the batch and refuse compressed bodies ([b036652](https://github.com/aethera-deeptech/backend/commit/b036652bd246f1c07250b750690f05805d856515))
* **gateway:** check an overload and a surge of one batch on their own ([b62c4b8](https://github.com/aethera-deeptech/backend/commit/b62c4b8ee1f2c7f5d69a1c2d79532292b6112ab1))
* **gateway:** keep the sequence on a new key, answer an old number with the next one and lower the body limit ([4a14deb](https://github.com/aethera-deeptech/backend/commit/4a14debe5407c38f8ecf6259931feab449db988d))
* **gateway:** look for a surge in every hour that a batch covers ([47a1c9d](https://github.com/aethera-deeptech/backend/commit/47a1c9d509cae4f60400148e2d8c0e52b8c6d32c))
* **gateway:** name the key error invalid_gateway_key, never null the last seen time and lock the claim by business ([3aa6bc0](https://github.com/aethera-deeptech/backend/commit/3aa6bc0e2ac0f5b310bf8f38deb3d5658b94978b))
* **gateway:** pass a context in the test helper and test the body limit under the server limit ([a98b987](https://github.com/aethera-deeptech/backend/commit/a98b9874c96a71d21df26a3911c40f80ab169a91))
* **gateway:** run the session check before the route handlers ([c9c5009](https://github.com/aethera-deeptech/backend/commit/c9c5009a22aa95b4c2ae175a05154b0d2f1a3370))
* **gateway:** trust the counter only over one hour, guard the hourly values and refuse zones that are not whole hours ([af44a50](https://github.com/aethera-deeptech/backend/commit/af44a5059f0a785a8a122160f23b060200017476))
* **mail:** retry without holding a worker and keep mail server text out of the log ([d154602](https://github.com/aethera-deeptech/backend/commit/d15460215b7fe51f045b4a1d439be993d94f6015))
* **metrics:** count the real wait of the pool and say who can reach the endpoint ([9ad8fed](https://github.com/aethera-deeptech/backend/commit/9ad8feda6094ce777a4102bb1886ec53c8c7a6e5))
* **metrics:** pass the error on, count every 429, read the pool once and keep group paths out of the labels ([4a7cf71](https://github.com/aethera-deeptech/backend/commit/4a7cf71bec18b7f6f3f5822bbab36b8f0120994d))
* **production:** decide the unit in the statement, bound the age of a day and answer required for a missing field ([be3f3c9](https://github.com/aethera-deeptech/backend/commit/be3f3c9d85fd183a9fc2a1177f2a86f36cc4fd75))
* **production:** run the session check before the route handlers ([be56297](https://github.com/aethera-deeptech/backend/commit/be5629788e7f6ebdef74418df8b9bf60bc22e11c))
* **production:** update the unit on every save so concurrent saves wait for each other ([ddc238a](https://github.com/aethera-deeptech/backend/commit/ddc238af8192d08d7e21ae1cab461c6d1510bc3e))
* **report:** count the days of a check between calendar dates ([fa49f01](https://github.com/aethera-deeptech/backend/commit/fa49f01be03ca171c0094fee945ed44050f728a8))
* **report:** cut a device name by its width only ([398fc06](https://github.com/aethera-deeptech/backend/commit/398fc06305c440a21761bb2f553ed35ccc9ca9b4))
* **report:** probe once per device and fix the PDF for long names, a rise in energy and the contract ([8e30e77](https://github.com/aethera-deeptech/backend/commit/8e30e775343fa325037b759adc65150c828ecdf3))
* **server:** reuse the readiness answer for two seconds and log a failed ping ([28ecef8](https://github.com/aethera-deeptech/backend/commit/28ecef835f6c3686887d98f4a17091a04587fd74))
* **testutil:** give a request of a test 15 seconds instead of 1 ([e389fcd](https://github.com/aethera-deeptech/backend/commit/e389fcd01d19041c69ae44c32ab712ff2d235137))
* **testutil:** give a request of a test 15 seconds instead of 1 ([104e988](https://github.com/aethera-deeptech/backend/commit/104e988850475194557a0ffb7b6e457cde89b315))
* **testutil:** let a test raise the time of a request ([693b62e](https://github.com/aethera-deeptech/backend/commit/693b62e5a17f423e129f64a52d32664f0556615a))
* **tips:** bound the phase query and refresh without a second connection or a lock on a cached read ([a764764](https://github.com/aethera-deeptech/backend/commit/a764764162994ac88f0a95e2303e1c1ea5d86931))
* **tips:** drop the unused field and make the tests follow the business name and the month ([8ab971a](https://github.com/aethera-deeptech/backend/commit/8ab971a1038d30afd2a6f7385e335c1236584dec))
* **tips:** give neutral advice when the check found no drop in energy ([3102cd4](https://github.com/aethera-deeptech/backend/commit/3102cd4031b99242e4dc85c30e117030e30a97d9))
* **tips:** lock the business row without blocking the inserts that point at it ([9238ace](https://github.com/aethera-deeptech/backend/commit/9238ace015d1b79b54db93e1ae4ce8df9d4d602e))

## 0.1.0 (2026-11-01)


### Features

* **cmd:** add the healthcheck command for a container without curl ([621898d](https://github.com/aethera-deeptech/backend/commit/621898df60428431478b125dd60ff1d3b177edc8))
* **cmd:** start the service with a graceful shutdown on a stop signal ([27cfc55](https://github.com/aethera-deeptech/backend/commit/27cfc558db1dbf6f70f54407f1b103399a1a15a5))
* **config:** add the Go module and a loader for the settings of the service ([a9397cb](https://github.com/aethera-deeptech/backend/commit/a9397cb090c54f1883c417b6711f3f42098306e4))
* **deploy:** add the production compose example and the rolling update script ([ff959bc](https://github.com/aethera-deeptech/backend/commit/ff959bc7389b0d5fa6e0f7b6dc666e18abeade6a))
* **deploy:** add the production compose example for two API copies ([e7d5c57](https://github.com/aethera-deeptech/backend/commit/e7d5c576d0ce5866c417b96e14c68d3b19d50f32))
* **deploy:** add the rolling update script that updates one copy at a time ([5c1d782](https://github.com/aethera-deeptech/backend/commit/5c1d782681fbbc09ea32b5e27ae237022c95cbb6))
* **image:** add the Dockerfile, the healthcheck command and a Hadolint job ([2d88d0f](https://github.com/aethera-deeptech/backend/commit/2d88d0f93e239935453f62edf80e4db3ec2c48c5))
* **server:** add the application with a health route and JSON logging ([9100529](https://github.com/aethera-deeptech/backend/commit/91005295bf1690b5d71dbc2e252a1d60464b4047))
* **service:** add the Go service skeleton with a health route ([9ebcc2a](https://github.com/aethera-deeptech/backend/commit/9ebcc2a6859b488ed7fc5bfcccfc8bab1d0d0401))


### Bug Fixes

* **ci:** put the license check step back on its own line ([e4ef681](https://github.com/aethera-deeptech/backend/commit/e4ef681c37bb5405db7be9e439ba53c60d0238ee))
* **cmd:** do not follow redirects in the healthcheck and log why it failed ([8c2edd2](https://github.com/aethera-deeptech/backend/commit/8c2edd24e2384a327fe2ca1e9f31c91e46255b11))
* **cmd:** log a fatal error at the error level and free the stop signal after it arrives ([b1db151](https://github.com/aethera-deeptech/backend/commit/b1db1518e021ca8908c1417bdedcfa6084e2d83e))
* **deploy:** keep the lock path, ports and env file of the updater consistent with compose ([7f46cb8](https://github.com/aethera-deeptech/backend/commit/7f46cb8f8474af24d440a43f4a27d6e319469f5b))
* **deploy:** leave IMAGE_TAG empty in the example so no version is started by mistake ([94f1912](https://github.com/aethera-deeptech/backend/commit/94f19129d868d5790d8fac397d287f3ecf49a819))
* **deploy:** report which copies run which version, pull what compose starts and time the health wait by the clock ([902c547](https://github.com/aethera-deeptech/backend/commit/902c547a815de3a31c9f66ee5e2df3c079b5c1bc))
* **deps:** update x/net, x/crypto and x/text to fix three high CVEs ([2105d1f](https://github.com/aethera-deeptech/backend/commit/2105d1fcabe770017613da241f79145272ed0c5e))
* **deps:** update x/net, x/crypto and x/text to the releases that fix three high CVEs ([457eafc](https://github.com/aethera-deeptech/backend/commit/457eafc2255c9d740b012ff08169d9b057b68e36))
* keep a Go source file named coverage.go out of the ignore rule ([7bbbad0](https://github.com/aethera-deeptech/backend/commit/7bbbad0de770f76dc6e8951dbdb89a44ed595a9d))
* **server:** set read, write and idle timeouts on the connections ([d83ab62](https://github.com/aethera-deeptech/backend/commit/d83ab6250d8498dba0af3de102ef05e437f6d140))
