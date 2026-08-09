# Changelog

## [0.2.3](https://github.com/cedricziel/truenas-mcp/compare/v0.2.2...v0.2.3) (2026-08-09)


### Bug Fixes

* say that app.update replaces the whole config ([#14](https://github.com/cedricziel/truenas-mcp/issues/14)) ([c99ea50](https://github.com/cedricziel/truenas-mcp/commit/c99ea50ccfaffa0b7d31b58ff5242c8fba00e747))

## [0.2.2](https://github.com/cedricziel/truenas-mcp/compare/v0.2.1...v0.2.2) (2026-08-09)


### Bug Fixes

* address agent feedback on the tool surface ([#12](https://github.com/cedricziel/truenas-mcp/issues/12)) ([0024eb9](https://github.com/cedricziel/truenas-mcp/commit/0024eb992144b73be58b100b70fa3cf4781d9489))

## [0.2.1](https://github.com/cedricziel/truenas-mcp/compare/v0.2.0...v0.2.1) (2026-08-08)


### Documentation

* sync the specs and archive the change ([#10](https://github.com/cedricziel/truenas-mcp/issues/10)) ([8038f1c](https://github.com/cedricziel/truenas-mcp/commit/8038f1c746fefc92116002323eba46a016656e07))

## [0.2.0](https://github.com/cedricziel/truenas-mcp/compare/v0.1.2...v0.2.0) (2026-08-08)


### Features

* serve MCP over stdio ([#9](https://github.com/cedricziel/truenas-mcp/issues/9)) ([81d1143](https://github.com/cedricziel/truenas-mcp/commit/81d1143cd490e0d258d51b3b75b0b553238ccbf0))


### Build and Packaging

* publish release binaries with GoReleaser ([#7](https://github.com/cedricziel/truenas-mcp/issues/7)) ([7deb012](https://github.com/cedricziel/truenas-mcp/commit/7deb01202c838745c7716d772ec305d6755a0d54))

## [0.1.2](https://github.com/cedricziel/truenas-mcp/compare/v0.1.1...v0.1.2) (2026-08-08)


### Build and Packaging

* drop the fields the registry rejects for OCI packages ([#5](https://github.com/cedricziel/truenas-mcp/issues/5)) ([630e17b](https://github.com/cedricziel/truenas-mcp/commit/630e17b23acf7ef3eb4b089614e7882a147994b6))

## [0.1.1](https://github.com/cedricziel/truenas-mcp/compare/v0.1.0...v0.1.1) (2026-08-08)


### Build and Packaging

* describe the server for the MCP registry ([#3](https://github.com/cedricziel/truenas-mcp/issues/3)) ([d7e2afb](https://github.com/cedricziel/truenas-mcp/commit/d7e2afb3dcaa32eb74812034262bb704e704f4ca))

## 0.1.0 (2026-08-08)


### Features

* **config:** load and validate configuration from the environment ([5a155f8](https://github.com/cedricziel/truenas-mcp/commit/5a155f88a273d316cb1fcb8b2138572324861724))
* **mcp:** add entity, job, and documentation resources ([cc74e43](https://github.com/cedricziel/truenas-mcp/commit/cc74e437c2b360aa1a8e2c72074a16283ff5e099))
* **mcp:** add the app lifecycle write tier and jobs tool ([d5d936b](https://github.com/cedricziel/truenas-mcp/commit/d5d936b34577504ce24a55a33eacf2f2fcfaf9ff))
* **mcp:** declare a complete annotation set on every tool ([df40681](https://github.com/cedricziel/truenas-mcp/commit/df40681421bff86402908100fad9875cf38b0990))
* **mcp:** expose search_methods, describe_method, and call_method ([fc18a9e](https://github.com/cedricziel/truenas-mcp/commit/fc18a9e7a3321ca544672fc720c3b8926d48a087))
* **mcp:** expose share and ACL configuration ([885065a](https://github.com/cedricziel/truenas-mcp/commit/885065a4dca365bcec8cd0997cb27cb798396e69))
* **mcp:** run every tool under the caller's own TrueNAS credential ([caf70e2](https://github.com/cedricziel/truenas-mcp/commit/caf70e2aa54af1a0b519b8eaca7bed80de793c03))
* **mcp:** serve MCP over the Streamable HTTP transport ([7e4322c](https://github.com/cedricziel/truenas-mcp/commit/7e4322c8af841de21df51588a3529518474774f3))
* re-establish a session whose connection has died ([86d31fc](https://github.com/cedricziel/truenas-mcp/commit/86d31fcae842bfada709f7971df7885bd6a5a130))
* serve the health endpoint over a configurable transport ([d225064](https://github.com/cedricziel/truenas-mcp/commit/d225064a994874434725577f4658233d56e7a92c))
* **server:** add a self-probing health check for the container ([09b3f93](https://github.com/cedricziel/truenas-mcp/commit/09b3f93d007fe7edb997cb18dd5cbd93b1b96373))
* **server:** report readiness as a health signal ([5775c41](https://github.com/cedricziel/truenas-mcp/commit/5775c41c7d05c09e49104e3744cd91857292f592))
* **tools:** add allowlist-gated method discovery ([7bb08aa](https://github.com/cedricziel/truenas-mcp/commit/7bb08aa19c6290bb2930d2354d70c25068d50d09))
* **tools:** add an apps containers operation ([4ae7213](https://github.com/cedricziel/truenas-mcp/commit/4ae7213da7f2ce8fdd3c86213d349f46437ebc1d))
* **tools:** add sharing, virtualization, backup, and filesystem reads ([543fd4c](https://github.com/cedricziel/truenas-mcp/commit/543fd4cc653053f4e13e89cbe54b24e67e1ed317))
* **tools:** add storage, system, and apps read concerns ([fb6cdc3](https://github.com/cedricziel/truenas-mcp/commit/fb6cdc3ddb973ffb2ff46454c6cb195a10d37cd0))
* **tools:** add the unrecoverable-operation denylist ([759ebbc](https://github.com/cedricziel/truenas-mcp/commit/759ebbc8c1fa41e50cb15d808475354d5d26b52a))
* **tools:** allow individually established unroled reads ([60e4332](https://github.com/cedricziel/truenas-mcp/commit/60e43320d1ccb9e839a16cdd460728b5f010d303))
* **tools:** gate discovery on RBAC metadata instead of name shapes ([4cc42f7](https://github.com/cedricziel/truenas-mcp/commit/4cc42f721be84fe17f9efe7510f554f6bc565ab3))
* **truenas:** add job tracking ([a720776](https://github.com/cedricziel/truenas-mcp/commit/a7207769bdb5c08389fb4a299f9b16f7869b30b3))
* **truenas:** add the JSON-RPC middleware client ([bcc330a](https://github.com/cedricziel/truenas-mcp/commit/bcc330a90337d26bbd09e812a9e14e420156dff6))
* **truenas:** detect the target version and refuse unsupported releases ([6d75817](https://github.com/cedricziel/truenas-mcp/commit/6d7581721c3774b3bcdd14ef0188c8e6085f8e0d))


### Bug Fixes

* **mcp:** carry a path argument on the read dispatch input ([c1f5130](https://github.com/cedricziel/truenas-mcp/commit/c1f51306c7df276e6b06461489e72e31744e1d05))
* **mcp:** give every output schema property an object schema ([ac88b7c](https://github.com/cedricziel/truenas-mcp/commit/ac88b7c2eabc688fa2b5aab6e115278e5d31c419))
* **tools:** require an app name for outdated_images ([9e9c012](https://github.com/cedricziel/truenas-mcp/commit/9e9c01223783e50b555fd3e47c17fc710894b175))
* **tools:** summarise inside the object parameter, not around it ([4933da7](https://github.com/cedricziel/truenas-mcp/commit/4933da7efb1c2626f71de6339f629b260c106aa2))


### Build and Packaging

* add container image and CI pipeline ([6348c80](https://github.com/cedricziel/truenas-mcp/commit/6348c80ac23625ade0b2127009a5fbac808d3870))
* add the TrueNAS custom app definition ([f361999](https://github.com/cedricziel/truenas-mcp/commit/f36199964bb858e4af1cb511cc6c9a9876c42db1))
* disable target certificate verification in the app definition ([8051cb4](https://github.com/cedricziel/truenas-mcp/commit/8051cb4a6ac08f59836d812cf5803e61344e3833))
* enable the write tier on the hive deployment ([ce87073](https://github.com/cedricziel/truenas-mcp/commit/ce870734d993a4a10d18181d09475cf60ae85600))


### Documentation

* add README ([e4eadcc](https://github.com/cedricziel/truenas-mcp/commit/e4eadccf4fe5454913d4ff072a97e06fdcbbc463))
* correct the plaintext rationale in the app definition ([f7bf717](https://github.com/cedricziel/truenas-mcp/commit/f7bf717b3f75c05e858f5671a4bcc0d27c4da531))
* describe roles-based discovery and what stays withheld ([39ffbf9](https://github.com/cedricziel/truenas-mcp/commit/39ffbf9e83398a67052fdaf2bf5ee13f7d67cb97))
* describe the release flow and the token it needs ([78700e9](https://github.com/cedricziel/truenas-mcp/commit/78700e98a7b673366b04b6c16958595487b4c4cd))
* document resources and update the status ([5e50696](https://github.com/cedricziel/truenas-mcp/commit/5e5069640638d591a957d9945bf379596878b823))
* document the discovery escape hatch ([2ef3128](https://github.com/cedricziel/truenas-mcp/commit/2ef3128e0a3444edfe0b9bac579abdf78c9626d2))
* document the new read concerns and share/ACL configuration ([f1120ba](https://github.com/cedricziel/truenas-mcp/commit/f1120ba565a56105a0633c274dae878f10af1dd9))
* document the write tier and the denylist ([2309765](https://github.com/cedricziel/truenas-mcp/commit/2309765168a568dfbc228743c90f9b11e53c77ac))
* explain the annotation set and why defaults are unsafe ([dd44c4a](https://github.com/cedricziel/truenas-mcp/commit/dd44c4a6a760df1267fc66933df4111479516192))
* list the read tools in the README ([ca79b56](https://github.com/cedricziel/truenas-mcp/commit/ca79b567bac384110299c02eed2a45219c9dcde6))
* note that the apps decision reads are per-app ([4d72470](https://github.com/cedricziel/truenas-mcp/commit/4d72470d3161a73abd78978035c1f43cd9c8a177))
* note the containers operation and why logs are absent ([6b6669b](https://github.com/cedricziel/truenas-mcp/commit/6b6669b59edc2bcc06394c68652374f89bc579c2))
* **openspec:** accept a TLS-terminating proxy as a valid topology ([945269a](https://github.com/cedricziel/truenas-mcp/commit/945269a4d0dd74ca0c337b85a071331103953972))
* **openspec:** ground the design against the live target ([49f1768](https://github.com/cedricziel/truenas-mcp/commit/49f17684fe54f3cd0fc29940fcb00d5c4e76e53b))
* **openspec:** mark the HTTP transport task complete ([39a7527](https://github.com/cedricziel/truenas-mcp/commit/39a752728996916d5e64c0072c4af1738fd8cde2))
* **openspec:** record implementation outcomes and resolved questions ([9aff273](https://github.com/cedricziel/truenas-mcp/commit/9aff273d8e64488d737badda60f38e29437aade6))
* update status to reflect the completed surface ([21bd0ca](https://github.com/cedricziel/truenas-mcp/commit/21bd0ca4e5b9816548fdfb160fd4b090f9a8a8a2))
