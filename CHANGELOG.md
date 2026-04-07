# Changelog

## [0.3.13](https://github.com/musher-dev/musher-cli/compare/v0.3.12...v0.3.13) (2026-04-07)


### Features

* add workflow package, scrollback config, scroll capture, and agent skills ([2e4c2d4](https://github.com/musher-dev/musher-cli/commit/2e4c2d4754bc928a4b79533b24a8e57beb1b287f))
* centralize config, consolidate packages, harden CI ([16c0801](https://github.com/musher-dev/musher-cli/commit/16c0801a13d4aa1ceccf3cbda5f838c4363729aa))
* centralize config, consolidate packages, harden CI, and add fuzz tests ([d4f248e](https://github.com/musher-dev/musher-cli/commit/d4f248e868c44d84252bc5c2d0a674e2fe9d3c72))
* **env:** centralize env var access through internal/env wrapper ([7d000a0](https://github.com/musher-dev/musher-cli/commit/7d000a003ea8a4e3b4e0639eb5d99867270dba43))
* **env:** centralize env var access through internal/env wrapper ([c659cce](https://github.com/musher-dev/musher-cli/commit/c659ccef1171b08b412b73713abf40f33a25eb4c))


### Bug Fixes

* prevent data race on color.NoColor in parallel tests ([d218b37](https://github.com/musher-dev/musher-cli/commit/d218b37e545156521ccd94663aee6a03ca0e2786))

## [0.3.12](https://github.com/musher-dev/musher-cli/compare/v0.3.11...v0.3.12) (2026-04-05)


### Features

* add harness runtime executors, health checks, and bundle swap UI ([1ab1376](https://github.com/musher-dev/musher-cli/commit/1ab137659f152fbdbbaaa36ddc1ba7c16f90be24))
* add harness runtime executors, health checks, and bundle swap UI ([f1e8785](https://github.com/musher-dev/musher-cli/commit/f1e87859d34997bfc614205b1463a09bc5a392ce)), closes [#52](https://github.com/musher-dev/musher-cli/issues/52) [#56](https://github.com/musher-dev/musher-cli/issues/56)


### Bug Fixes

* normalize CRLF in skill frontmatter and close logger in test ([5fd2e29](https://github.com/musher-dev/musher-cli/commit/5fd2e293558cc32784ac1dd7e9c472eceedd59b6))
* use cross-platform binary in passthrough cancellation test ([fa6f4f9](https://github.com/musher-dev/musher-cli/commit/fa6f4f9734eb435398eba2e9b38ace2878d734ce))

## [0.3.11](https://github.com/musher-dev/musher-cli/compare/v0.3.10...v0.3.11) (2026-04-04)


### Features

* **tui:** polish input styling and add test coverage ([3524519](https://github.com/musher-dev/musher-cli/commit/352451997b9db183fa270f20291bbfa0742de097))
* **tui:** polish input styling and add test coverage ([b003848](https://github.com/musher-dev/musher-cli/commit/b003848700fae42039db2ad9f66e32189bb29b3c))

## [0.3.10](https://github.com/musher-dev/musher-cli/compare/v0.3.9...v0.3.10) (2026-04-03)


### Bug Fixes

* correct Codex harness skill and agent discovery ([56302bb](https://github.com/musher-dev/musher-cli/commit/56302bb2885aeb36bb0d8b2097ad40571d52ed49))
* correct Codex harness skill and agent discovery ([042b165](https://github.com/musher-dev/musher-cli/commit/042b1653b98778b94f5c75110728c6e24fe20204))
* handle JSON agents and ensure required Codex TOML fields ([2a89f6b](https://github.com/musher-dev/musher-cli/commit/2a89f6b51c33e6a70b08ef7e005c7bd02c6bfc58))
* handle regular file blocking directory creation in cwd mode ([bd69376](https://github.com/musher-dev/musher-cli/commit/bd693765753ae62418468a09fc3791adb02121ab))

## [0.3.9](https://github.com/musher-dev/musher-cli/compare/v0.3.8...v0.3.9) (2026-04-03)


### Features

* add agent injection support and test infrastructure ([78a854a](https://github.com/musher-dev/musher-cli/commit/78a854a12f05b25dd685f4dd69b51e43d71cfbf6))
* add agent injection support and test infrastructure ([44d5d72](https://github.com/musher-dev/musher-cli/commit/44d5d72c756215c0b6934d94731c54ca7caccd79))


### Bug Fixes

* pass -SkipCertificateCheck to Invoke-WebRequest on PowerShell 7+ ([a67636a](https://github.com/musher-dev/musher-cli/commit/a67636a3cc4651fac7ad4b9c46cf4527816ff7ed))
* use context.WithTimeout instead of nonexistent time.WithTimeout ([de27039](https://github.com/musher-dev/musher-cli/commit/de2703942e610018e01fdce1534ce40fe69e091a))

## [0.3.8](https://github.com/musher-dev/musher-cli/compare/v0.3.7...v0.3.8) (2026-04-02)


### Features

* add bundle pack command for local testing ([b5b2501](https://github.com/musher-dev/musher-cli/commit/b5b250139aef4705d9704f90a9ff964146c7443e))
* add bundle pack command for local testing ([9c4f176](https://github.com/musher-dev/musher-cli/commit/9c4f176f07121ad3f6ee293c0285430289ace461))


### Bug Fixes

* set TMP/TEMP env vars in runtime root test for Windows ([ace90e9](https://github.com/musher-dev/musher-cli/commit/ace90e93122cd254fb11224a899e401527cc5f34))

## [0.3.7](https://github.com/musher-dev/musher-cli/compare/v0.3.6...v0.3.7) (2026-04-01)


### Features

* JSON output mode, multi-file skills, and CLI polish ([d10c9fe](https://github.com/musher-dev/musher-cli/commit/d10c9feb87d5a3fd7e9fc604aab1c12630f340d4))
* JSON output mode, multi-file skills, and CLI polish ([097d434](https://github.com/musher-dev/musher-cli/commit/097d4345daa334c01d910d0fa4bfed57645d3641)), closes [#48](https://github.com/musher-dev/musher-cli/issues/48) [#46](https://github.com/musher-dev/musher-cli/issues/46)


### Performance Improvements

* **ci:** parallelize CI pipeline and fix NilAway timeout ([58f9f7f](https://github.com/musher-dev/musher-cli/commit/58f9f7fe045e4b3adbf41498725b04b3fa5ecb8f))

## [0.3.6](https://github.com/musher-dev/musher-cli/compare/v0.3.5...v0.3.6) (2026-03-31)


### Features

* add --no-tui persistent flag and consumer group constant ([8def4fb](https://github.com/musher-dev/musher-cli/commit/8def4fb51a2f8652808f9552ad62693ec0226c86))
* add cache management and consumer CLI foundations ([317e876](https://github.com/musher-dev/musher-cli/commit/317e8767a67d40ac88df2ab1f42f0ee28757c510))
* add consumer error constructors and path functions ([45626bc](https://github.com/musher-dev/musher-cli/commit/45626bc736129553402e8efef7a2efd472888311))
* add OCI pull path, TUI screens for auth/config/validate/push, and form components ([f07726c](https://github.com/musher-dev/musher-cli/commit/f07726cac8c0f4a1354fe9f11f42abe10331a84d))
* add package foundations for consumer CLI merge ([f0f4547](https://github.com/musher-dev/musher-cli/commit/f0f4547e4972cde3fcc6db2b7b26d80fd39cb709))
* add search/load consumer commands with TUI and claude harness provider ([7c31ee7](https://github.com/musher-dev/musher-cli/commit/7c31ee76f0a2426d3436cbfb9b7d72b475bc7182))
* **cache:** add cache management commands and content-addressable store ([3cc257c](https://github.com/musher-dev/musher-cli/commit/3cc257cb3694fa637d9f8055967275e0e1538e60))
* **client:** add bundle resolve, pull, and asset download endpoints ([0ed3a08](https://github.com/musher-dev/musher-cli/commit/0ed3a08bfd3efc658ae322e3ee0787710118e60e))
* **config:** add musher config list/get/set commands ([958505f](https://github.com/musher-dev/musher-cli/commit/958505f11a416a8fd850d173e1e9d452178b2f34))
* **harness:** add all 6 harness providers to registry ([e6e1339](https://github.com/musher-dev/musher-cli/commit/e6e133994b5046f9a7ffaa5eec7373b01e92adfe))
* **quality:** add comprehensive quality gates, linters, and 70%+ test coverage ([dfbf3e1](https://github.com/musher-dev/musher-cli/commit/dfbf3e16b4e6a2e1c88e476325081d8eb29f42ff))
* **run:** add musher run command for end-to-end bundle execution ([f7b9882](https://github.com/musher-dev/musher-cli/commit/f7b9882362a69f26fa3a771a8b8e4a871090d415))
* **testutil:** add integration test framework and dev:test-live task ([90c19db](https://github.com/musher-dev/musher-cli/commit/90c19db828b0884226a3aa6af0c3ca21dab63ec7))
* **testutil:** add shared test helpers package ([f16ee44](https://github.com/musher-dev/musher-cli/commit/f16ee44039e09142b84960f1401ffeb0946d3698))
* **tui:** add interactive home screen with Tokyo Night theme ([0820b87](https://github.com/musher-dev/musher-cli/commit/0820b871db430adfd2be46dd4343f4dbc90647f0))
* **tui:** expand home screen with USE/CREATE/MANAGE sections and status panel ([22c69f9](https://github.com/musher-dev/musher-cli/commit/22c69f9f16e7ca2343a4fefb6de4c4cdf223fc5f))
* **tui:** refactor search screen with centering, sliding window, and ref input ([259c6d9](https://github.com/musher-dev/musher-cli/commit/259c6d985dbc8d2ccaaeb621bae9baa4dd7f17ff))
* **update:** integrate background update agent into CLI lifecycle ([6a5eef8](https://github.com/musher-dev/musher-cli/commit/6a5eef80a3c8fa7c6c47e43dd926bbec5b5c4a85))


### Bug Fixes

* **ci:** pin nilaway version and reduce memory pressure in CI ([5cb4bc0](https://github.com/musher-dev/musher-cli/commit/5cb4bc094d6cdddcfa25f71b8f2820a1dcbf8f63))
* **ci:** replace printf %f with echo in coverage check for dash compat ([38a93a6](https://github.com/musher-dev/musher-cli/commit/38a93a66ac5005947201dd00d4ff88bfc426c26f))
* **ci:** resolve nilaway FieldByLabel nil panic and restore coverage floor ([e29bca2](https://github.com/musher-dev/musher-cli/commit/e29bca209aee8b2b5f72a0e6b559e402cf4d9a53))
* **test:** resolve macOS CI test failures in auth and paths ([d831400](https://github.com/musher-dev/musher-cli/commit/d831400aac42a82112b97ce149901cac5dc088f3))
* **test:** resolve Windows CI test failures in paths and probe ([92b5b0d](https://github.com/musher-dev/musher-cli/commit/92b5b0d9533ce60c5e9f157138a2f08067c674e7))
* **test:** skip NeedsElevation test on Windows ([0379190](https://github.com/musher-dev/musher-cli/commit/0379190a4ba6c6d1b60a954974c09b1b3fca09c3))

## [0.3.5](https://github.com/musher-dev/musher-cli/compare/v0.3.4...v0.3.5) (2026-03-28)


### Features

* align CLI with unified publish API and add yank --yes flag ([f454d4c](https://github.com/musher-dev/musher-cli/commit/f454d4cf26f68e87c0b2b5b0b571b1beb88cf570))
* align CLI with unified publish API and add yank --yes flag ([f191ff6](https://github.com/musher-dev/musher-cli/commit/f191ff6fe4aa9b25e8ee46095fc4e50c301c28ca))

## [0.3.4](https://github.com/musher-dev/musher-cli/compare/v0.3.3...v0.3.4) (2026-03-27)


### Features

* group auth commands, add remove command, and improve push UX ([6940881](https://github.com/musher-dev/musher-cli/commit/6940881d9ce5e03c09ad5ed10e18f6590b350256))
* group auth commands, add remove command, and improve push UX ([0df77c9](https://github.com/musher-dev/musher-cli/commit/0df77c9014497f4852dc7eaa38b7aebca5c6088c))

## [0.3.3](https://github.com/musher-dev/musher-cli/compare/v0.3.2...v0.3.3) (2026-03-27)


### Bug Fixes

* restore canonical go.sum to pass mod tidy check ([993c2e7](https://github.com/musher-dev/musher-cli/commit/993c2e7cbffd685ae5e2c36bc841ceaf32a8b98e))

## [0.3.2](https://github.com/musher-dev/musher-cli/compare/v0.3.1...v0.3.2) (2026-03-24)


### Features

* add `musher add` command and enhance init with interactive login ([46ce888](https://github.com/musher-dev/musher-cli/commit/46ce8887607ef6bde548a2ffb5494621c1d6ce6f))
* add musher add command and enhance init with interactive login ([81a2588](https://github.com/musher-dev/musher-cli/commit/81a25880b1045b52a8e4a3f3245155674b074943))

## [0.3.1](https://github.com/musher-dev/musher-cli/compare/v0.3.0...v0.3.1) (2026-03-23)


### Features

* add pull command to download bundles from registry ([17d4ea2](https://github.com/musher-dev/musher-cli/commit/17d4ea2155010c67697c6ba7b367526ce95fc43f))
* add pull command to download bundles from registry ([7e46299](https://github.com/musher-dev/musher-cli/commit/7e462997a82e37c466a2a0ce6d64960856961157))

## [0.3.0](https://github.com/musher-dev/musher-cli/compare/v0.2.2...v0.3.0) (2026-03-22)


### ⚠ BREAKING CHANGES

* musher.yaml field `publisher` renamed to `namespace`, version refs now use `:` separator (ns/slug:version) instead of `@`.

### Features

* add --publish-to-hub flag, asset kind aliases, and improved error messages ([c0946a3](https://github.com/musher-dev/musher-cli/commit/c0946a303cd17fce1e859d690834bd916dcb553d))
* add --publish-to-hub flag, asset kind aliases, and improved error messages ([cd81f00](https://github.com/musher-dev/musher-cli/commit/cd81f00170c8601a201d551f658e543acee95800))
* add hub subcommands, bundledef package, schemas, and pack/skills internals ([67e8be4](https://github.com/musher-dev/musher-cli/commit/67e8be4efd001e9e8b808021f455c6f4170d7003))
* add import command for skills from npm and local directories ([7b7e2dd](https://github.com/musher-dev/musher-cli/commit/7b7e2ddd39457ff2daf02d5988e6df5f8f2d9f6a))
* add visibility recovery flow and hub-readiness validation ([bdc9ca8](https://github.com/musher-dev/musher-cli/commit/bdc9ca89f5901b74a967c6e91afd39d6ec42b917))
* enrich hub listing creation, remove star/unstar and user profile endpoints ([999e1c4](https://github.com/musher-dev/musher-cli/commit/999e1c4debb9a83d47d62cbe961a7c125e7d9236))
* initial musher CLI scaffold ([2bc990b](https://github.com/musher-dev/musher-cli/commit/2bc990bd0f00739f8e7ed3d174e0b96239313385))
* overhaul init command with templates, rename publisher to namespace in hub commands ([42a0348](https://github.com/musher-dev/musher-cli/commit/42a0348df95a6f0c04ca19e735d6cd2da37c1582))
* rename asset types, camelCase push payload, parse RFC 9457 errors, relax schema limits ([1459f65](https://github.com/musher-dev/musher-cli/commit/1459f65d2c1b34a23951044ff71daa24f6b10eab))
* use GET /v1/publisher/me for identity across login, whoami, and init ([4d709db](https://github.com/musher-dev/musher-cli/commit/4d709dbdaae0cff3c4c04b0446361d83a590d4d9))


### Bug Fixes

* add visibility and license to init templates, improve publish error hints ([b448da2](https://github.com/musher-dev/musher-cli/commit/b448da28be8b0559dd77bbc135250ef08c5688eb)), closes [#13](https://github.com/musher-dev/musher-cli/issues/13)
* **ci:** add [@latest](https://github.com/latest) to go install commands in Taskfile ([071c867](https://github.com/musher-dev/musher-cli/commit/071c8677dbf03ebfd7c1f5ede0e9d332a9bb460b))
* correct help text terminology and examples across CLI commands ([94ea2ce](https://github.com/musher-dev/musher-cli/commit/94ea2ce0ab28e51d7c3618d4f5dd766c52a71fd2))
* improve error messages for missing arguments and remove import/pack commands ([910f2c4](https://github.com/musher-dev/musher-cli/commit/910f2c4b429c6993a2364025f58bad8930b5b750))
* isolate init tests from host credentials ([8d35efd](https://github.com/musher-dev/musher-cli/commit/8d35efda1ba4656a87a8611c5ad13cb88c3440fe))
* make init template private by default, remove public-only fields ([98fb795](https://github.com/musher-dev/musher-cli/commit/98fb795655c735933d13240fee79d56ca2cf4167))
* use colon separator in version display for copy-pastable refs ([c1ab86f](https://github.com/musher-dev/musher-cli/commit/c1ab86f53dd412ee3770dedaedeee5f550544e64))
* use errors.Is for wrapped os.ErrNotExist checks ([869765b](https://github.com/musher-dev/musher-cli/commit/869765b63bf0dd1129ef2ae700d6418b437ee31e))
* use standard Unix permissions for project files created by init and import ([4c1b6c2](https://github.com/musher-dev/musher-cli/commit/4c1b6c264d76bb4e6e55ca44d29b7d48c6c72bb7))
* warn when running under sudo, improve auth error hint ([e134866](https://github.com/musher-dev/musher-cli/commit/e1348661932adf946d1098ac6168fc69a2d37a66))


### Code Refactoring

* reshape CLI for v1 GA — rename publisher to namespace, remove hub commands, add unyank ([87ed4bb](https://github.com/musher-dev/musher-cli/commit/87ed4bbcee54ac3c94c899aa501771ab6592ae1d))

## [0.2.2](https://github.com/musher-dev/musher-cli/compare/v0.2.1...v0.2.2) (2026-03-22)


### Features

* add --publish-to-hub flag, asset kind aliases, and improved error messages ([c0946a3](https://github.com/musher-dev/musher-cli/commit/c0946a303cd17fce1e859d690834bd916dcb553d))
* add --publish-to-hub flag, asset kind aliases, and improved error messages ([cd81f00](https://github.com/musher-dev/musher-cli/commit/cd81f00170c8601a201d551f658e543acee95800))

## [0.2.1](https://github.com/musher-dev/musher-cli/compare/v0.2.0...v0.2.1) (2026-03-20)


### Features

* add visibility recovery flow and hub-readiness validation ([bdc9ca8](https://github.com/musher-dev/musher-cli/commit/bdc9ca89f5901b74a967c6e91afd39d6ec42b917))

## [0.2.0](https://github.com/musher-dev/musher-cli/compare/v0.1.0...v0.2.0) (2026-03-20)


### ⚠ BREAKING CHANGES

* musher.yaml field `publisher` renamed to `namespace`, version refs now use `:` separator (ns/slug:version) instead of `@`.

### Features

* add hub subcommands, bundledef package, schemas, and pack/skills internals ([67e8be4](https://github.com/musher-dev/musher-cli/commit/67e8be4efd001e9e8b808021f455c6f4170d7003))
* add import command for skills from npm and local directories ([7b7e2dd](https://github.com/musher-dev/musher-cli/commit/7b7e2ddd39457ff2daf02d5988e6df5f8f2d9f6a))
* enrich hub listing creation, remove star/unstar and user profile endpoints ([999e1c4](https://github.com/musher-dev/musher-cli/commit/999e1c4debb9a83d47d62cbe961a7c125e7d9236))
* initial musher CLI scaffold ([2bc990b](https://github.com/musher-dev/musher-cli/commit/2bc990bd0f00739f8e7ed3d174e0b96239313385))
* overhaul init command with templates, rename publisher to namespace in hub commands ([42a0348](https://github.com/musher-dev/musher-cli/commit/42a0348df95a6f0c04ca19e735d6cd2da37c1582))
* rename asset types, camelCase push payload, parse RFC 9457 errors, relax schema limits ([1459f65](https://github.com/musher-dev/musher-cli/commit/1459f65d2c1b34a23951044ff71daa24f6b10eab))
* use GET /v1/publisher/me for identity across login, whoami, and init ([4d709db](https://github.com/musher-dev/musher-cli/commit/4d709dbdaae0cff3c4c04b0446361d83a590d4d9))


### Bug Fixes

* add visibility and license to init templates, improve publish error hints ([b448da2](https://github.com/musher-dev/musher-cli/commit/b448da28be8b0559dd77bbc135250ef08c5688eb)), closes [#13](https://github.com/musher-dev/musher-cli/issues/13)
* **ci:** add [@latest](https://github.com/latest) to go install commands in Taskfile ([071c867](https://github.com/musher-dev/musher-cli/commit/071c8677dbf03ebfd7c1f5ede0e9d332a9bb460b))
* correct help text terminology and examples across CLI commands ([94ea2ce](https://github.com/musher-dev/musher-cli/commit/94ea2ce0ab28e51d7c3618d4f5dd766c52a71fd2))
* improve error messages for missing arguments and remove import/pack commands ([910f2c4](https://github.com/musher-dev/musher-cli/commit/910f2c4b429c6993a2364025f58bad8930b5b750))
* isolate init tests from host credentials ([8d35efd](https://github.com/musher-dev/musher-cli/commit/8d35efda1ba4656a87a8611c5ad13cb88c3440fe))
* make init template private by default, remove public-only fields ([98fb795](https://github.com/musher-dev/musher-cli/commit/98fb795655c735933d13240fee79d56ca2cf4167))
* use colon separator in version display for copy-pastable refs ([c1ab86f](https://github.com/musher-dev/musher-cli/commit/c1ab86f53dd412ee3770dedaedeee5f550544e64))
* use errors.Is for wrapped os.ErrNotExist checks ([869765b](https://github.com/musher-dev/musher-cli/commit/869765b63bf0dd1129ef2ae700d6418b437ee31e))
* use standard Unix permissions for project files created by init and import ([4c1b6c2](https://github.com/musher-dev/musher-cli/commit/4c1b6c264d76bb4e6e55ca44d29b7d48c6c72bb7))
* warn when running under sudo, improve auth error hint ([e134866](https://github.com/musher-dev/musher-cli/commit/e1348661932adf946d1098ac6168fc69a2d37a66))


### Code Refactoring

* reshape CLI for v1 GA — rename publisher to namespace, remove hub commands, add unyank ([87ed4bb](https://github.com/musher-dev/musher-cli/commit/87ed4bbcee54ac3c94c899aa501771ab6592ae1d))

## Changelog
