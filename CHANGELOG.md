# Changelog

## [0.9.0](https://github.com/ai-outfitter/agent-operator/compare/agent-operator-v0.8.0...agent-operator-v0.9.0) (2026-08-25)


### Features

* **deploy-catalog:** add IAM bootstrap template ([fb3aff5](https://github.com/ai-outfitter/agent-operator/commit/fb3aff586ace19d91f661e13ab8ac29f8b73f2c2))
* **deploy-catalog:** standardize AWS identities ([2467310](https://github.com/ai-outfitter/agent-operator/commit/2467310eff3bc805d74ca1e72f0f7e9c7c59b8f0))

## [0.8.0](https://github.com/ai-outfitter/agent-operator/compare/agent-operator-v0.7.0...agent-operator-v0.8.0) (2026-08-20)


### Features

* add a Helm chart for the operator ([466dac2](https://github.com/ai-outfitter/agent-operator/commit/466dac2d3b80f419f34463dbf82b899454885e95))
* **agent:** project GitHub notification settings ([#48](https://github.com/ai-outfitter/agent-operator/issues/48)) ([7c0ce5b](https://github.com/ai-outfitter/agent-operator/commit/7c0ce5b4a03491a4f0b30fcccce57d941c99d76e))
* **agent:** project typed channel selection ([#50](https://github.com/ai-outfitter/agent-operator/issues/50)) ([eabe25a](https://github.com/ai-outfitter/agent-operator/commit/eabe25a00c941bade1b9dacd52f6802ab914c332))
* **deploy-catalog:** select agents by cluster, not only by glob ([#46](https://github.com/ai-outfitter/agent-operator/issues/46)) ([ea7e070](https://github.com/ai-outfitter/agent-operator/commit/ea7e0706297d4884a457df0cc6236011a349f021))
* **operator:** add Helm chart ([ab9ed9a](https://github.com/ai-outfitter/agent-operator/commit/ab9ed9a654eea196767c0c89e20ff0f951dcd38c))


### Bug Fixes

* **deploy-catalog:** budget the converge wait per agent ([#43](https://github.com/ai-outfitter/agent-operator/issues/43)) ([b861cfa](https://github.com/ai-outfitter/agent-operator/commit/b861cfa2aad35cc9a0fc21a2d08ff136f0760fef))

## [0.7.0](https://github.com/ai-outfitter/agent-operator/compare/agent-operator-v0.6.0...agent-operator-v0.7.0) (2026-08-13)


### Features

* **actions:** add the deploy-catalog composite action ([b422849](https://github.com/ai-outfitter/agent-operator/commit/b4228496b6b3dfd26b98c68e49c433f5d631b0c1)), closes [#39](https://github.com/ai-outfitter/agent-operator/issues/39)


### Bug Fixes

* **agent-image:** ship an ssh client in the dev runtime image ([a02b604](https://github.com/ai-outfitter/agent-operator/commit/a02b6049c2f60a768700e8103ed07e1991549957))

## [0.6.0](https://github.com/ai-outfitter/agent-operator/compare/agent-operator-v0.5.0...agent-operator-v0.6.0) (2026-08-07)


### Features

* **operator:** stable session identity and closure-gated nix-store machinery ([#34](https://github.com/ai-outfitter/agent-operator/issues/34)) ([6c22238](https://github.com/ai-outfitter/agent-operator/commit/6c22238befeee22bc0044dfc14d9095f6197b62a))

## [0.5.0](https://github.com/ai-outfitter/agent-operator/compare/agent-operator-v0.4.0...agent-operator-v0.5.0) (2026-08-04)


### Features

* default agents to the published Outfitter container ([#32](https://github.com/ai-outfitter/agent-operator/issues/32)) ([d31fb64](https://github.com/ai-outfitter/agent-operator/commit/d31fb64380e553e3db56bd05e8bfd8410c86264d))
* publish only the controller image ([#28](https://github.com/ai-outfitter/agent-operator/issues/28)) ([8aca7c4](https://github.com/ai-outfitter/agent-operator/commit/8aca7c47f7c5e7a7b340e03866aa93f939627381))

## [0.4.0](https://github.com/ai-outfitter/agent-operator/compare/agent-operator-v0.3.0...agent-operator-v0.4.0) (2026-08-03)


### ⚠ BREAKING CHANGES

* the API group, CRD names, namespace, image names, and runtime environment variables all change; existing clusters must recreate CRs under aioutfitter.com and redeploy. CHANGELOG.md and wiki/ sources keep historical names. The pre-rename nonprod deployment still runs legacy identifiers; the runbook documents the substitution until it is redeployed.

### Features

* add a Chrome DevTools browser sidecar to agent Pods ([#19](https://github.com/ai-outfitter/agent-operator/issues/19)) ([7abf52e](https://github.com/ai-outfitter/agent-operator/commit/7abf52ea38fb76404ba77742b1344c733b6f7126))
* add agent session gateway support ([#10](https://github.com/ai-outfitter/agent-operator/issues/10)) ([a8ab147](https://github.com/ai-outfitter/agent-operator/commit/a8ab1475d0ef6810ee9a4a2cd62c2a571812a247))
* add generic scheduled agent runtime ([d5ffe5d](https://github.com/ai-outfitter/agent-operator/commit/d5ffe5dd05c10796f6793493aab06740d0fc32ff))
* add local Stalwart JMAP mail demo ([4fecfab](https://github.com/ai-outfitter/agent-operator/commit/4fecfaba498f0c09f02c14f5fc0e86380c4d8c94))
* add nonprod Slack agent deployment ([20320e1](https://github.com/ai-outfitter/agent-operator/commit/20320e1c5dea3ec8c825756fdec445ac92db5eee))
* allow agents to select runtime images ([#14](https://github.com/ai-outfitter/agent-operator/issues/14)) ([69535bf](https://github.com/ai-outfitter/agent-operator/commit/69535bf7bf5f785abfdc1ef2a5a7afe0eaf3ab53))
* **operator:** reconcile organization and agent primitives ([58929f6](https://github.com/ai-outfitter/agent-operator/commit/58929f68e9f8a0252a2c3508e4f97920838c76c9))
* use Channels for resident agents ([34f90a5](https://github.com/ai-outfitter/agent-operator/commit/34f90a5ecf00927a22323eed2dfafa2e498bae12))


### Bug Fixes

* **ci:** give the image publish job devenv's binary cache ([#24](https://github.com/ai-outfitter/agent-operator/issues/24)) ([1cb64dc](https://github.com/ai-outfitter/agent-operator/commit/1cb64dcbd1f15f2ad736ee1c5c665c4de66f3b94))
* compose release image destinations ([0f07cfc](https://github.com/ai-outfitter/agent-operator/commit/0f07cfc5445edd15a20dd7f41a3da09b76b63343))
* **operator:** harden the browser sidecar — token isolation, digest pin, regression tests ([#22](https://github.com/ai-outfitter/agent-operator/issues/22)) ([4d4e5b6](https://github.com/ai-outfitter/agent-operator/commit/4d4e5b6ef8a9b72512b65847d2a3b3a2a096b84c))
* publish release images reliably ([d5fb5a9](https://github.com/ai-outfitter/agent-operator/commit/d5fb5a93df28a7c29f98343fbf4b84a95fc50714))
* reconcile agents with missing credentials ([ed7b43e](https://github.com/ai-outfitter/agent-operator/commit/ed7b43e5e6055b9bf83fa66cd20e29a2a0b7f698))
* run headless-shell directly, bypassing the image entrypoint ([#21](https://github.com/ai-outfitter/agent-operator/issues/21)) ([173362c](https://github.com/ai-outfitter/agent-operator/commit/173362cd3910cf4aa5775960acaa3a53cacef5ce))
* update the operator module hash ([4dc2abc](https://github.com/ai-outfitter/agent-operator/commit/4dc2abc858013b470d90fe232d3af9ac977922e1))
* use release automation token ([7e5b979](https://github.com/ai-outfitter/agent-operator/commit/7e5b979e2c2aaef9cdda23aad1be1ffc6ef7a7d7))


### Code Refactoring

* remove legacy link naming for agent-operator ([#17](https://github.com/ai-outfitter/agent-operator/issues/17)) ([78a3c81](https://github.com/ai-outfitter/agent-operator/commit/78a3c819b6ebecbbb3488fbd5ccd9cf9f442aca0))

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
