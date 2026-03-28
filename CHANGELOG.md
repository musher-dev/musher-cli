# Changelog

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
