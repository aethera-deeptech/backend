# Changelog

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
