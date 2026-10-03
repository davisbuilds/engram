# Changelog

## [0.4.0](https://github.com/davisbuilds/engram/compare/v0.3.0...v0.4.0) (2026-10-03)


### Features

* **config:** shared_index turns Claude's shared memory index off ([#40](https://github.com/davisbuilds/engram/issues/40)) ([4ffb831](https://github.com/davisbuilds/engram/commit/4ffb831a08cc5592b4b2b08f503e8a544823ff6c))

## [0.3.0](https://github.com/davisbuilds/engram/compare/v0.2.0...v0.3.0) (2026-10-02)


### ⚠ BREAKING CHANGES

* **cli:** reject malformed arguments, disable unlisted harnesses, envelope schema 2, curate timeout ([#26](https://github.com/davisbuilds/engram/issues/26))

### Features

* **cli:** migrate --shared retires the originals of shared memories ([#38](https://github.com/davisbuilds/engram/issues/38)) ([4c8bae8](https://github.com/davisbuilds/engram/commit/4c8bae8aa3473f9d11ed8192db60f24b7a98babf))
* **cli:** retire memories with forget, tombstones, orphan reports and detach ([#27](https://github.com/davisbuilds/engram/issues/27)) ([b3841b1](https://github.com/davisbuilds/engram/commit/b3841b1d52b02142ddcd1b763358513aed840989))
* **cli:** share narrows by cwd with --applies-cwd and --any-cwd ([#31](https://github.com/davisbuilds/engram/issues/31)) ([e0b7877](https://github.com/davisbuilds/engram/commit/e0b7877a6f2e46e2196802e6269e572d58249ab0))
* **import:** merge base: fast-forward native edits, canonical_ahead, and --keep ([#28](https://github.com/davisbuilds/engram/issues/28)) ([70cde38](https://github.com/davisbuilds/engram/commit/70cde38f19713d8d6191330c982d763779cc1a93))
* **sync:** import edits made in place to Claude's shared renders ([#37](https://github.com/davisbuilds/engram/issues/37)) ([589973c](https://github.com/davisbuilds/engram/commit/589973c3fb28ce00478d58d986b4293b33e4b941))
* **sync:** render Claude's shared memories once and import their index in every project ([#33](https://github.com/davisbuilds/engram/issues/33)) ([6e9769d](https://github.com/davisbuilds/engram/commit/6e9769d0ba26bf70b95bf028d54771afdad5249a))


### Bug Fixes

* **cli:** follow the main repository for Claude slugs and project scopes ([#34](https://github.com/davisbuilds/engram/issues/34)) ([f3b4a45](https://github.com/davisbuilds/engram/commit/f3b4a455677973e12ff714a8395bbed26e3bb924))
* **cli:** keep a settled scope quietly when a provisional import changes nothing ([#35](https://github.com/davisbuilds/engram/issues/35)) ([cf498a9](https://github.com/davisbuilds/engram/commit/cf498a9515a015a3405f332e67d84d3746052d9e))
* **cli:** reject malformed arguments, disable unlisted harnesses, envelope schema 2, curate timeout ([#26](https://github.com/davisbuilds/engram/issues/26)) ([e52a3b6](https://github.com/davisbuilds/engram/commit/e52a3b6859c8d44747cacea21e38ce1bdfc38252))
* **cli:** show claude-code lists the shared renders too ([#36](https://github.com/davisbuilds/engram/issues/36)) ([e8d789f](https://github.com/davisbuilds/engram/commit/e8d789fa07f463f71c784c9efefaa3e5fd0ac861))
* **import:** strip Codex consolidator echoes of engram notes ([#39](https://github.com/davisbuilds/engram/issues/39)) ([970766f](https://github.com/davisbuilds/engram/commit/970766fd4cb354d972fc61768ef7fa4625c7ca0a))
* **sync:** keep a re-tiered memory's Codex note out of other projects' stale sweep ([#32](https://github.com/davisbuilds/engram/issues/32)) ([888f9cc](https://github.com/davisbuilds/engram/commit/888f9cca160fc7497fae42e2d56c6c4ad0401979))
* **sync:** keep Codex notes another project's run rendered ([#23](https://github.com/davisbuilds/engram/issues/23)) ([8f40b34](https://github.com/davisbuilds/engram/commit/8f40b3455dcb5841a2510d3fd01f02fb5d9095a9))

## [0.2.0](https://github.com/davisbuilds/engram/compare/v0.1.0...v0.2.0) (2026-09-26)


### Features

* **reconcile:** on-demand cross-harness enricher command ([#9](https://github.com/davisbuilds/engram/issues/9)) ([5781acf](https://github.com/davisbuilds/engram/commit/5781acf98aa02ebf6fb7c19afc41646bda489bc8))
* **sync:** self-documenting MEMORY.md header ([#10](https://github.com/davisbuilds/engram/issues/10)) ([d1a97a5](https://github.com/davisbuilds/engram/commit/d1a97a57c98de264cd2408527a17113258a3b68a))


### Bug Fixes

* harden curate parsing, CLI flags and Codex import ([#20](https://github.com/davisbuilds/engram/issues/20)) ([30f12db](https://github.com/davisbuilds/engram/commit/30f12dbf6e38f414a2cc5a69a633daa42965193d))
* **import:** never silently re-scope a memory from live-filesystem state ([#11](https://github.com/davisbuilds/engram/issues/11)) ([62ab51b](https://github.com/davisbuilds/engram/commit/62ab51b5c47bca345abfbf1190a71803738e5e99))
* **import:** stop provenance-only differences conflicting; explain conflicts; add --refresh ([#17](https://github.com/davisbuilds/engram/issues/17)) ([b382938](https://github.com/davisbuilds/engram/commit/b3829382387b77bcf2efaf2453930b7974cc48f6))
* stop tests and sync from deleting or overwriting memory ([#19](https://github.com/davisbuilds/engram/issues/19)) ([93a1fab](https://github.com/davisbuilds/engram/commit/93a1faba4ba134f7598f67a06fb5a23e18353f31))
