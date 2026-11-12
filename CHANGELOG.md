# Changelog

## [0.2.0](https://github.com/dvalfrid/proton-mail-mcp/compare/v0.1.0...v0.2.0) (2026-11-12)


### Features

* error messages say what to do and link to a troubleshooting page ([#63](https://github.com/dvalfrid/proton-mail-mcp/issues/63)) ([c6443f8](https://github.com/dvalfrid/proton-mail-mcp/commit/c6443f80d95e65094c64032381d70c738902c994))
* **guard:** cap the messages the organize tools change per server run ([#76](https://github.com/dvalfrid/proton-mail-mcp/issues/76)) ([06ec263](https://github.com/dvalfrid/proton-mail-mcp/commit/06ec263c62070e46380da5f5e1bcec0226be71cb))
* **tools:** mark_read, mark_unread, flag and unflag ([#56](https://github.com/dvalfrid/proton-mail-mcp/issues/56)) ([e2e82f9](https://github.com/dvalfrid/proton-mail-mcp/commit/e2e82f93c356d41fdd2ee74e5def8d5e706ce0f5))
* **tools:** move_message and add_label ([#57](https://github.com/dvalfrid/proton-mail-mcp/issues/57)) ([827d0b3](https://github.com/dvalfrid/proton-mail-mcp/commit/827d0b3ed0d1c20cacad88c79d3d282bf5a38881))
* **tools:** trash_message, move to Trash and never delete ([#58](https://github.com/dvalfrid/proton-mail-mcp/issues/58)) ([7fb3271](https://github.com/dvalfrid/proton-mail-mcp/commit/7fb32715560631aeb4f23ec18a8c0912cf3a2e29))


### Bug Fixes

* **imap:** say when Bridge can't reach Proton, and that a change may be partly done ([#70](https://github.com/dvalfrid/proton-mail-mcp/issues/70)) ([58a51f3](https://github.com/dvalfrid/proton-mail-mcp/commit/58a51f31ec532e6533123d9685c28731f2fe56ed))
* **mail:** a crafted HTML email can no longer make read_message run for minutes ([#78](https://github.com/dvalfrid/proton-mail-mcp/issues/78)) ([bc97098](https://github.com/dvalfrid/proton-mail-mcp/commit/bc970982b86fd24e5aa496ad0eee06d0b8052fb6))
* **mail:** keep sender text out of the error for a malformed header ([#79](https://github.com/dvalfrid/proton-mail-mcp/issues/79)) ([766a0b2](https://github.com/dvalfrid/proton-mail-mcp/commit/766a0b2bf63692e6c2449ce6ccfe6fbe91b9f0c5))
* **release:** tag releases vX.Y.Z, without the component name ([#48](https://github.com/dvalfrid/proton-mail-mcp/issues/48)) ([e661a0d](https://github.com/dvalfrid/proton-mail-mcp/commit/e661a0d96d7198c2bed8225c625b3488d88202a4))
* **server:** recover panics so a bug fails one call, not the server ([#61](https://github.com/dvalfrid/proton-mail-mcp/issues/61)) ([56d55eb](https://github.com/dvalfrid/proton-mail-mcp/commit/56d55ebcd1e5e9c28d9d0857a03cfc9419df6b58))
* **setup:** trust a signed bridge.exe only when it is in Program Files ([#77](https://github.com/dvalfrid/proton-mail-mcp/issues/77)) ([fb2ecfa](https://github.com/dvalfrid/proton-mail-mcp/commit/fb2ecfa2f2ec1b89d95e447e39e16969d5c068e5))
* the review's informational findings (one pinned certificate, docs, CI) ([#81](https://github.com/dvalfrid/proton-mail-mcp/issues/81)) ([e49b56a](https://github.com/dvalfrid/proton-mail-mcp/commit/e49b56adddcebfb27a9b00c91a672b31b419f176))
* the review's low findings (bidi characters, Spam, setup advice, keyring errors) ([#80](https://github.com/dvalfrid/proton-mail-mcp/issues/80)) ([ff16326](https://github.com/dvalfrid/proton-mail-mcp/commit/ff16326292d690b78e6d183d614449bfebc0555b))

## 0.1.0 (2026-11-12)


### Features

* **check:** diagnostics command and clear startup errors ([#38](https://github.com/dvalfrid/proton-mail-mcp/issues/38)) ([27d63ad](https://github.com/dvalfrid/proton-mail-mcp/commit/27d63ad7bb1d6829959aa3fa9879d3508786eacd))
* **mail:** parse messages safely into capped plain text ([#15](https://github.com/dvalfrid/proton-mail-mcp/issues/15)) ([d9eb140](https://github.com/dvalfrid/proton-mail-mcp/commit/d9eb140edc720e52ac8e3a5664344872af422022))
* **setup:** cross-platform setup command without hand-edited files ([#41](https://github.com/dvalfrid/proton-mail-mcp/issues/41)) ([f9ef7ae](https://github.com/dvalfrid/proton-mail-mcp/commit/f9ef7aee1e4727ce3fcb84210521e4d100a98585))
* **tools:** list folders and messages read-only through bridge ([#5](https://github.com/dvalfrid/proton-mail-mcp/issues/5)) ([20e56bf](https://github.com/dvalfrid/proton-mail-mcp/commit/20e56bf2c36bd48e07bcf455ac82376dac9a0fd4))
* **tools:** read one message as untrusted content ([#16](https://github.com/dvalfrid/proton-mail-mcp/issues/16)) ([1851ec5](https://github.com/dvalfrid/proton-mail-mcp/commit/1851ec59877a488798ddb684f99a5f99e67b72a3))
* **tools:** search messages in a folder read-only ([#17](https://github.com/dvalfrid/proton-mail-mcp/issues/17)) ([e9fcb7b](https://github.com/dvalfrid/proton-mail-mcp/commit/e9fcb7b3ec0386aaab81565d1f3fc6462a9c7204))


### Bug Fixes

* **imap:** order list and search results by date, not uid ([#35](https://github.com/dvalfrid/proton-mail-mcp/issues/35)) ([cb28033](https://github.com/dvalfrid/proton-mail-mcp/commit/cb28033727264dc894afd2aec6fbac72f366fba0))
* **imap:** refuse a uid after the folder was resynced (uidvalidity) ([#37](https://github.com/dvalfrid/proton-mail-mcp/issues/37)) ([8438c12](https://github.com/dvalfrid/proton-mail-mcp/commit/8438c129d1eecc754b7519052dc5953a63b92e63))
* **release:** make the first release 0.1.0 instead of 1.0.0 ([#11](https://github.com/dvalfrid/proton-mail-mcp/issues/11)) ([3b5e133](https://github.com/dvalfrid/proton-mail-mcp/commit/3b5e133329f10a07307c757b4d4a409f4481ef1a))
* **setup:** register in the config that Claude Desktop (Store) reads ([#43](https://github.com/dvalfrid/proton-mail-mcp/issues/43)) ([bb0a510](https://github.com/dvalfrid/proton-mail-mcp/commit/bb0a510fcca7075c0528a02d5e7ac70f148ae831))
* **testutil:** stop the test IMAP server logging after the test ends ([#46](https://github.com/dvalfrid/proton-mail-mcp/issues/46)) ([b58c872](https://github.com/dvalfrid/proton-mail-mcp/commit/b58c8724a52828d1db22cb7c7c563967bffb14ab))
* **tools:** tell Claude that search misses non-ASCII terms on Bridge ([#36](https://github.com/dvalfrid/proton-mail-mcp/issues/36)) ([c7361be](https://github.com/dvalfrid/proton-mail-mcp/commit/c7361be07a145f008a94a131ce7dff6b0bc1d101))


### Performance Improvements

* **imap:** give searches 55 s and advise narrowing on a timeout ([#44](https://github.com/dvalfrid/proton-mail-mcp/issues/44)) ([4961028](https://github.com/dvalfrid/proton-mail-mcp/commit/49610289c33a829f88e38c00171b0430add690b0))

## Changelog

All notable changes are documented here. This file is generated by [Release Please](https://github.com/googleapis/release-please) from [Conventional Commit](https://www.conventionalcommits.org/) subjects: don't edit released sections by hand.
