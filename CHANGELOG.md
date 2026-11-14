# Changelog

## [0.5.0](https://github.com/dvalfrid/proton-mail-mcp/compare/v0.4.0...v0.5.0) (2026-11-14)


### Features

* **config:** keep the owner's options in config.json and show where each comes from ([#168](https://github.com/dvalfrid/proton-mail-mcp/issues/168)) ([32a14bc](https://github.com/dvalfrid/proton-mail-mcp/commit/32a14bcdee1dfb8754b3a13eb08fc9d7911a015d))
* **setup:** a Settings page in the setup window ([#169](https://github.com/dvalfrid/proton-mail-mcp/issues/169)) ([524d198](https://github.com/dvalfrid/proton-mail-mcp/commit/524d198d72c478efd1abfa4e329b91cd939ded2d))
* **tools:** mark messages as spam on the owner's request ([#165](https://github.com/dvalfrid/proton-mail-mcp/issues/165)) ([c5ace36](https://github.com/dvalfrid/proton-mail-mcp/commit/c5ace36d2c9b0f9a747a94847fe55bc043b59275))
* **tools:** write drafts from the owner's other addresses ([#167](https://github.com/dvalfrid/proton-mail-mcp/issues/167)) ([95ca319](https://github.com/dvalfrid/proton-mail-mcp/commit/95ca319587e855d431994492d0964a96c220e272))

## [0.4.0](https://github.com/dvalfrid/proton-mail-mcp/compare/v0.3.0...v0.4.0) (2026-11-14)


### Features

* **tools:** ask Claude to show a written draft laid out as an email ([#144](https://github.com/dvalfrid/proton-mail-mcp/issues/144)) ([b2d5ee8](https://github.com/dvalfrid/proton-mail-mcp/commit/b2d5ee831a85779d7eb4a269807070b92c76a0ce))
* **tools:** fetch an image a message links to, on the owner's request ([#153](https://github.com/dvalfrid/proton-mail-mcp/issues/153)) ([aed995a](https://github.com/dvalfrid/proton-mail-mcp/commit/aed995adede3e56136a22c5768568e10e7ff1418))
* **tools:** let Claude view images in a message ([#117](https://github.com/dvalfrid/proton-mail-mcp/issues/117)) ([bf77f6e](https://github.com/dvalfrid/proton-mail-mcp/commit/bf77f6e88c2a855cc5bd94c691b93f9af31a5051))
* **tools:** let trash_message move drafts on Bridge 3.23.1 or later ([#135](https://github.com/dvalfrid/proton-mail-mcp/issues/135)) ([35419b2](https://github.com/dvalfrid/proton-mail-mcp/commit/35419b2eb58b2a3639bb40e3ae31b3a77d0c2cc6))
* **tools:** remove a label from messages ([#136](https://github.com/dvalfrid/proton-mail-mcp/issues/136)) ([b451746](https://github.com/dvalfrid/proton-mail-mcp/commit/b45174689736d02439c34581b9663ab463881491))
* **tools:** replace a draft and move the old one to Trash ([#133](https://github.com/dvalfrid/proton-mail-mcp/issues/133)) ([92a524c](https://github.com/dvalfrid/proton-mail-mcp/commit/92a524c06fb0c9450dffa36765dcddc31f0e5ea7))
* **tools:** save an attachment to the owner's attachment folder ([#119](https://github.com/dvalfrid/proton-mail-mcp/issues/119)) ([61ba6e7](https://github.com/dvalfrid/proton-mail-mcp/commit/61ba6e7b7846741e63f9d062e8930b96e4357ddf))
* **tools:** show a written draft's body in a quote block ([#147](https://github.com/dvalfrid/proton-mail-mcp/issues/147)) ([3611c67](https://github.com/dvalfrid/proton-mail-mcp/commit/3611c67e90f69997bc7ec11955fdec2c270c24c9))
* **tools:** tell the owner the running version and the tools in short ([#142](https://github.com/dvalfrid/proton-mail-mcp/issues/142)) ([b22a3e2](https://github.com/dvalfrid/proton-mail-mcp/commit/b22a3e276950587c126be463fba7a97e627d9fdf))
* **tools:** write a new email as a draft in Drafts ([#131](https://github.com/dvalfrid/proton-mail-mcp/issues/131)) ([4efd282](https://github.com/dvalfrid/proton-mail-mcp/commit/4efd282643738b35641a84f29dc114533c58dd3b))
* **tools:** write a reply to a message as a draft ([#138](https://github.com/dvalfrid/proton-mail-mcp/issues/138)) ([b838cad](https://github.com/dvalfrid/proton-mail-mcp/commit/b838cad326286b9ce40ea0c19132e6dca553cdb5))


### Bug Fixes

* **installer:** plain ASCII finish text that fits on the page ([#155](https://github.com/dvalfrid/proton-mail-mcp/issues/155)) ([dd27c25](https://github.com/dvalfrid/proton-mail-mcp/commit/dd27c2562ce0f4185447925fc30b3216f0f6e380))
* **installer:** say how to restart Claude after an update ([#146](https://github.com/dvalfrid/proton-mail-mcp/issues/146)) ([b23ef03](https://github.com/dvalfrid/proton-mail-mcp/commit/b23ef03f059172f1f13c3e3e80425df27cd93cf0))
* **installer:** update while Claude runs copies from several earlier versions ([#140](https://github.com/dvalfrid/proton-mail-mcp/issues/140)) ([b7110b1](https://github.com/dvalfrid/proton-mail-mcp/commit/b7110b1bd7b1216d179ec77f0d2bc7c8f2fe8626))
* **tools:** mark every image in an HTML message and list the remote ones ([#152](https://github.com/dvalfrid/proton-mail-mcp/issues/152)) ([1ad34e6](https://github.com/dvalfrid/proton-mail-mcp/commit/1ad34e6909f31ea425e9f663d7dada9a673fa2aa))
* **tools:** reply to own mail sent from an alias goes to its recipients ([#151](https://github.com/dvalfrid/proton-mail-mcp/issues/151)) ([9709abc](https://github.com/dvalfrid/proton-mail-mcp/commit/9709abce4db1b9687be0cb2237f5e4660022295a))
* **tools:** show dates and search days in the owner's time zone ([#121](https://github.com/dvalfrid/proton-mail-mcp/issues/121)) ([2bbbdcf](https://github.com/dvalfrid/proton-mail-mcp/commit/2bbbdcf6150ac542c62d115e7a086ce0c9e143cb))

## [0.3.0](https://github.com/dvalfrid/proton-mail-mcp/compare/v0.2.0...v0.3.0) (2026-11-12)


### Features

* **release:** a Windows installer, so updates keep Claude's registration ([#101](https://github.com/dvalfrid/proton-mail-mcp/issues/101)) ([89a7a2c](https://github.com/dvalfrid/proton-mail-mcp/commit/89a7a2c94deb862040b360501a45b3b146415bd5))
* **setup:** graphical setup wizard on Windows ([#92](https://github.com/dvalfrid/proton-mail-mcp/issues/92)) ([cb74a86](https://github.com/dvalfrid/proton-mail-mcp/commit/cb74a865f2dc16c3f2a88e3d85d90aceb20a22c8))
* **setup:** the project icon, in the wizard's window and in the repository ([#99](https://github.com/dvalfrid/proton-mail-mcp/issues/99)) ([1f3c6b6](https://github.com/dvalfrid/proton-mail-mcp/commit/1f3c6b6f9da30c15fd15441942e515a99465bed2))


### Bug Fixes

* **setup:** offer to switch Claude to this copy when it starts another ([#104](https://github.com/dvalfrid/proton-mail-mcp/issues/104)) ([cb2bde7](https://github.com/dvalfrid/proton-mail-mcp/commit/cb2bde7a87b86e2c280aea4052f00e1e8e2594d1))
* **setup:** the setup wizard no longer flashes white ([#95](https://github.com/dvalfrid/proton-mail-mcp/issues/95)) ([512a8d4](https://github.com/dvalfrid/proton-mail-mcp/commit/512a8d448ff55e5502adfa20dada1431540462d2))
* **setup:** the setup wizard no longer opens a terminal window ([#97](https://github.com/dvalfrid/proton-mail-mcp/issues/97)) ([9b110c5](https://github.com/dvalfrid/proton-mail-mcp/commit/9b110c530090682a25a19ae145727f3b36bad2c4))

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
