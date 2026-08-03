# Changelog

## [0.3.0](https://github.com/ai-outfitter/agent-operator/compare/link-operator-v0.2.3...link-operator-v0.3.0) (2026-08-03)


### Features

* add a Chrome DevTools browser sidecar to agent Pods ([#19](https://github.com/ai-outfitter/agent-operator/issues/19)) ([7abf52e](https://github.com/ai-outfitter/agent-operator/commit/7abf52ea38fb76404ba77742b1344c733b6f7126))
* add agent session gateway support ([#10](https://github.com/ai-outfitter/agent-operator/issues/10)) ([a8ab147](https://github.com/ai-outfitter/agent-operator/commit/a8ab1475d0ef6810ee9a4a2cd62c2a571812a247))
* allow agents to select runtime images ([#14](https://github.com/ai-outfitter/agent-operator/issues/14)) ([69535bf](https://github.com/ai-outfitter/agent-operator/commit/69535bf7bf5f785abfdc1ef2a5a7afe0eaf3ab53))


### Bug Fixes

* **operator:** harden the browser sidecar — token isolation, digest pin, regression tests ([#22](https://github.com/ai-outfitter/agent-operator/issues/22)) ([4d4e5b6](https://github.com/ai-outfitter/agent-operator/commit/4d4e5b6ef8a9b72512b65847d2a3b3a2a096b84c))
* run headless-shell directly, bypassing the image entrypoint ([#21](https://github.com/ai-outfitter/agent-operator/issues/21)) ([173362c](https://github.com/ai-outfitter/agent-operator/commit/173362cd3910cf4aa5775960acaa3a53cacef5ce))

## [0.2.3](https://github.com/ai-outfitter/link-operator/compare/link-operator-v0.2.2...link-operator-v0.2.3) (2026-07-25)


### Bug Fixes

* compose release image destinations ([0f07cfc](https://github.com/ai-outfitter/link-operator/commit/0f07cfc5445edd15a20dd7f41a3da09b76b63343))
* reconcile agents with missing credentials ([ed7b43e](https://github.com/ai-outfitter/link-operator/commit/ed7b43e5e6055b9bf83fa66cd20e29a2a0b7f698))

## [0.2.2](https://github.com/ai-outfitter/link-operator/compare/link-operator-v0.2.1...link-operator-v0.2.2) (2026-07-25)


### Bug Fixes

* update the operator module hash ([4dc2abc](https://github.com/ai-outfitter/link-operator/commit/4dc2abc858013b470d90fe232d3af9ac977922e1))

## [0.2.1](https://github.com/ai-outfitter/link-operator/compare/link-operator-v0.2.0...link-operator-v0.2.1) (2026-07-25)


### Bug Fixes

* publish release images reliably ([d5fb5a9](https://github.com/ai-outfitter/link-operator/commit/d5fb5a93df28a7c29f98343fbf4b84a95fc50714))

## [0.2.0](https://github.com/ai-outfitter/link-operator/compare/link-operator-v0.1.0...link-operator-v0.2.0) (2026-07-25)


### Features

* add generic scheduled agent runtime ([d5ffe5d](https://github.com/ai-outfitter/link-operator/commit/d5ffe5dd05c10796f6793493aab06740d0fc32ff))
* add local Stalwart JMAP mail demo ([4fecfab](https://github.com/ai-outfitter/link-operator/commit/4fecfaba498f0c09f02c14f5fc0e86380c4d8c94))
* add nonprod Slack agent deployment ([20320e1](https://github.com/ai-outfitter/link-operator/commit/20320e1c5dea3ec8c825756fdec445ac92db5eee))
* **operator:** reconcile organization and agent primitives ([58929f6](https://github.com/ai-outfitter/link-operator/commit/58929f68e9f8a0252a2c3508e4f97920838c76c9))
* use Channels for resident agents ([34f90a5](https://github.com/ai-outfitter/link-operator/commit/34f90a5ecf00927a22323eed2dfafa2e498bae12))


### Bug Fixes

* use release automation token ([7e5b979](https://github.com/ai-outfitter/link-operator/commit/7e5b979e2c2aaef9cdda23aad1be1ffc6ef7a7d7))
