<!-- markdownlint-disable MD024 -->
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- Update LogScheduleInfo function and related components by @yubiuser in [#821](https://github.com/nicholas-fedor/watchtower/pull/821)

### Chores

- Update module github.com/docker/docker to v28.5.2+incompatible by @renovate[bot] in [#884](https://github.com/nicholas-fedor/watchtower/pull/884)
- Update module github.com/docker/cli to v28.5.2+incompatible by @renovate[bot] in [#880](https://github.com/nicholas-fedor/watchtower/pull/880)
- Update dependency go to v1.25.4 by @renovate[bot] in [#879](https://github.com/nicholas-fedor/watchtower/pull/879)
- Update module github.com/nicholas-fedor/shoutrrr to v0.12.0 by @renovate[bot] in [#871](https://github.com/nicholas-fedor/watchtower/pull/871)
- Update module github.com/nicholas-fedor/shoutrrr to v0.11.1 by @renovate[bot] in [#848](https://github.com/nicholas-fedor/watchtower/pull/848)
- Update module github.com/onsi/ginkgo/v2 to v2.27.2 by @renovate[bot] in [#841](https://github.com/nicholas-fedor/watchtower/pull/841)
- Update module github.com/nicholas-fedor/shoutrrr to v0.11.0 by @renovate[bot] in [#832](https://github.com/nicholas-fedor/watchtower/pull/832)
- Update module github.com/onsi/ginkgo/v2 to v2.27.1 by @renovate[bot] in [#830](https://github.com/nicholas-fedor/watchtower/pull/830)
- Update module github.com/nicholas-fedor/shoutrrr to v0.10.3 by @renovate[bot] in [#814](https://github.com/nicholas-fedor/watchtower/pull/814)
- Update dependency go to v1.25.3 by @renovate[bot] in [#804](https://github.com/nicholas-fedor/watchtower/pull/804)
- Update module golang.org/x/text to v0.30.0 by @renovate[bot] in [#784](https://github.com/nicholas-fedor/watchtower/pull/784)
- Update module github.com/docker/docker to v28.5.1+incompatible by @renovate[bot] in [#783](https://github.com/nicholas-fedor/watchtower/pull/783)
- Update module github.com/docker/cli to v28.5.1+incompatible by @renovate[bot] in [#782](https://github.com/nicholas-fedor/watchtower/pull/782)
- Update dependency go to v1.25.2 by @renovate[bot] in [#779](https://github.com/nicholas-fedor/watchtower/pull/779)
- Update module github.com/nicholas-fedor/shoutrrr to v0.10.1 by @renovate[bot] in [#770](https://github.com/nicholas-fedor/watchtower/pull/770)

### Fixed

- fix: resolve notification buffer, formatting, and message issues by @nicholas-fedor in [#864](https://github.com/nicholas-fedor/watchtower/pull/864)
- Handle ghost containers in ListAllContainers by @nicholas-fedor in [#859](https://github.com/nicholas-fedor/watchtower/pull/859)
- Restore notifications for monitor-only containers by @nicholas-fedor in [#850](https://github.com/nicholas-fedor/watchtower/pull/850)
- Make shoutrrr notifier thread-safe by @nicholas-fedor in [#844](https://github.com/nicholas-fedor/watchtower/pull/844)
- Prevent double notification entries for unchanged containers by @nicholas-fedor in [#836](https://github.com/nicholas-fedor/watchtower/pull/836)
- Implement cleanup detection for update-on-start control by @nicholas-fedor in [#831](https://github.com/nicholas-fedor/watchtower/pull/831)
- Clear shoutrrr notification queue after sending by @nicholas-fedor in [#824](https://github.com/nicholas-fedor/watchtower/pull/824)
- Resolve data race in concurrent test logging by @nicholas-fedor in [#800](https://github.com/nicholas-fedor/watchtower/pull/800)
- Improve Podman detection reliability by @nicholas-fedor in [#799](https://github.com/nicholas-fedor/watchtower/pull/799)
- Resolve issues with notification split by container feature by @nicholas-fedor in [#775](https://github.com/nicholas-fedor/watchtower/pull/775)
- Improve WATCHTOWER_UPDATE_ON_START logging messages by @nicholas-fedor in [#768](https://github.com/nicholas-fedor/watchtower/pull/768)

## [1.12.1] - 2025-10-04

### Added

- Add --notification-split-by-container flag by @nicholas-fedor in [#721](https://github.com/nicholas-fedor/watchtower/pull/721)
- Add --cpu-copy-mode flag for Podman CPU compatibility by @nicholas-fedor in [#712](https://github.com/nicholas-fedor/watchtower/pull/712)
- Add UID and GID support for lifecycle hooks scripts by @nicholas-fedor in [#690](https://github.com/nicholas-fedor/watchtower/pull/690)
- Add --update-on-start flag for immediate update check by @nicholas-fedor in [#672](https://github.com/nicholas-fedor/watchtower/pull/672)
- Add health check waiting for rolling restarts by @nicholas-fedor in [#671](https://github.com/nicholas-fedor/watchtower/pull/671)
- Add container metadata for lifecycle hooks by @nicholas-fedor in [#670](https://github.com/nicholas-fedor/watchtower/pull/670)

### Changed

- Add HTTP API host configuration support by @nicholas-fedor in [#697](https://github.com/nicholas-fedor/watchtower/pull/697)
- Enhance HTTP API update endpoint with structured JSON response by @nicholas-fedor in [#673](https://github.com/nicholas-fedor/watchtower/pull/673)

### Chores

- Update module github.com/docker/cli to v28.5.0+incompatible by @renovate[bot] in [#750](https://github.com/nicholas-fedor/watchtower/pull/750)
- Update module github.com/docker/docker to v28.5.0+incompatible by @renovate[bot] in [#751](https://github.com/nicholas-fedor/watchtower/pull/751)
- Update module github.com/onsi/ginkgo/v2 to v2.26.0 by @renovate[bot] in [#749](https://github.com/nicholas-fedor/watchtower/pull/749)
- Update genproto dependencies by @nicholas-fedor in [#742](https://github.com/nicholas-fedor/watchtower/pull/742)
- Update module github.com/nicholas-fedor/shoutrrr to v0.10.0 by @renovate[bot] in [#727](https://github.com/nicholas-fedor/watchtower/pull/727)
- Update module github.com/nicholas-fedor/shoutrrr to v0.9.1 by @renovate[bot] in [#676](https://github.com/nicholas-fedor/watchtower/pull/676)
- Update module github.com/nicholas-fedor/shoutrrr to v0.9.0 by @renovate[bot] in [#674](https://github.com/nicholas-fedor/watchtower/pull/674)
- Update module github.com/spf13/viper to v1.21.0 by @renovate[bot] in [#650](https://github.com/nicholas-fedor/watchtower/pull/650)
- Update module golang.org/x/text to v0.29.0 by @renovate[bot] in [#645](https://github.com/nicholas-fedor/watchtower/pull/645)
- Update module github.com/prometheus/client_golang to v1.23.2 by @renovate[bot] in [#641](https://github.com/nicholas-fedor/watchtower/pull/641)
- Update module github.com/onsi/ginkgo/v2 to v2.25.3 by @renovate[bot] in [#637](https://github.com/nicholas-fedor/watchtower/pull/637)
- Update module github.com/prometheus/client_golang to v1.23.1 by @renovate[bot] in [#638](https://github.com/nicholas-fedor/watchtower/pull/638)

### Fixed

- Prevent I/O blocking in API update handler by @nicholas-fedor in [#765](https://github.com/nicholas-fedor/watchtower/pull/765)
- Digest retrieval failed, falling back to full pull by @nicholas-fedor in [#763](https://github.com/nicholas-fedor/watchtower/pull/763)
- Ensure Watchtower updates itself last to fix notification split issue by @nicholas-fedor in [#756](https://github.com/nicholas-fedor/watchtower/pull/756)
- Correct shutdown lock waiting logic and prevent test timeouts by @nicholas-fedor in [#753](https://github.com/nicholas-fedor/watchtower/pull/753)
- Resolve data race in shoutrrr notifications by @nicholas-fedor in [#746](https://github.com/nicholas-fedor/watchtower/pull/746)
- Prevent nil pointer dereference in container cleanup by @nicholas-fedor in [#745](https://github.com/nicholas-fedor/watchtower/pull/745)
- Integrate --update-on-start with normal update cycle by @nicholas-fedor in [#740](https://github.com/nicholas-fedor/watchtower/pull/740)
- Improve CI test reliability across platforms by @nicholas-fedor in [#732](https://github.com/nicholas-fedor/watchtower/pull/732)
- Prevent concurrent Docker client access causing crashes by @nicholas-fedor in [#731](https://github.com/nicholas-fedor/watchtower/pull/731)
- Resolve Docker Distribution API manifest HEAD request issues by @nicholas-fedor in [#728](https://github.com/nicholas-fedor/watchtower/pull/728)
- Improve self-update handling with robust digest parsing by @nicholas-fedor in [#724](https://github.com/nicholas-fedor/watchtower/pull/724)
- Address container identification issue by @nicholas-fedor in [#718](https://github.com/nicholas-fedor/watchtower/pull/718)
- Improve logging clarity and accuracy for scheduling modes by @nicholas-fedor in [#716](https://github.com/nicholas-fedor/watchtower/pull/716)
- Improve Container ID Retrieval for Self-Update by @nicholas-fedor in [#714](https://github.com/nicholas-fedor/watchtower/pull/714)
- Send report notifications in monitor-only mode by @nicholas-fedor in [#709](https://github.com/nicholas-fedor/watchtower/pull/709)
- Resolve scope issues in self-updates and improve digest request handling by @nicholas-fedor in [#683](https://github.com/nicholas-fedor/watchtower/pull/683)
- Improve digest fetching by falling back to GET when HEAD returns 404 by @nicholas-fedor in [#669](https://github.com/nicholas-fedor/watchtower/pull/669)
- Resolve HTTP API failures on multiple simultaneous requests by @nicholas-fedor in [#668](https://github.com/nicholas-fedor/watchtower/pull/668)
- Add HEAD to GET fallback for digest fetching by @nicholas-fedor in [#667](https://github.com/nicholas-fedor/watchtower/pull/667)
- Enhance scope isolation and self-update safeguards by @nicholas-fedor in [#666](https://github.com/nicholas-fedor/watchtower/pull/666)
- Prevent dereferencing an uninitialized notifier instance by @nicholas-fedor in [#644](https://github.com/nicholas-fedor/watchtower/pull/644)

## [1.11.8] - 2025-09-04

### Changed

- Overhaul documentation website by @nicholas-fedor in [#574](https://github.com/nicholas-fedor/watchtower/pull/574)

### Chores

- Update module github.com/nicholas-fedor/shoutrrr to v0.8.18 by @renovate[bot] in [#621](https://github.com/nicholas-fedor/watchtower/pull/621)
- Update module github.com/docker/docker to v28.4.0+incompatible by @renovate[bot] in [#619](https://github.com/nicholas-fedor/watchtower/pull/619)
- Update module github.com/docker/cli to v28.4.0+incompatible by @renovate[bot] in [#618](https://github.com/nicholas-fedor/watchtower/pull/618)
- Update dependency go to v1.25.1 by @renovate[bot] in [#616](https://github.com/nicholas-fedor/watchtower/pull/616)
- Update Go dependencies by @nicholas-fedor in [#612](https://github.com/nicholas-fedor/watchtower/pull/612)
- Update module github.com/spf13/pflag to v1.0.8 by @renovate[bot] in [#606](https://github.com/nicholas-fedor/watchtower/pull/606)
- Update module github.com/onsi/ginkgo/v2 to v2.25.2 by @renovate[bot] in [#599](https://github.com/nicholas-fedor/watchtower/pull/599)
- Update module github.com/stretchr/testify to v1.11.1 by @renovate[bot] in [#597](https://github.com/nicholas-fedor/watchtower/pull/597)
- Update module github.com/onsi/gomega to v1.38.2 by @renovate[bot] in [#594](https://github.com/nicholas-fedor/watchtower/pull/594)
- Update module github.com/stretchr/testify to v1.11.0 by @renovate[bot] in [#587](https://github.com/nicholas-fedor/watchtower/pull/587)
- Update module github.com/onsi/gomega to v1.38.1 by @renovate[bot] in [#584](https://github.com/nicholas-fedor/watchtower/pull/584)
- Update module github.com/onsi/ginkgo/v2 to v2.25.1 by @renovate[bot] in [#582](https://github.com/nicholas-fedor/watchtower/pull/582)
- Update module github.com/onsi/ginkgo/v2 to v2.25.0 by @renovate[bot] in [#571](https://github.com/nicholas-fedor/watchtower/pull/571)
- Update module github.com/onsi/ginkgo/v2 to v2.24.0 by @renovate[bot] in [#568](https://github.com/nicholas-fedor/watchtower/pull/568)
- Update dependency go to v1.25.0 by @renovate[bot] in [#544](https://github.com/nicholas-fedor/watchtower/pull/544)
- Update google.golang.org/genproto modules by @nicholas-fedor in [#529](https://github.com/nicholas-fedor/watchtower/pull/529)
- Update module github.com/nicholas-fedor/shoutrrr to v0.8.17 by @renovate[bot] in [#526](https://github.com/nicholas-fedor/watchtower/pull/526)
- Update module github.com/nicholas-fedor/shoutrrr to v0.8.16 by @renovate[bot] in [#524](https://github.com/nicholas-fedor/watchtower/pull/524)
- Update module golang.org/x/text to v0.28.0 by @renovate[bot] in [#523](https://github.com/nicholas-fedor/watchtower/pull/523)
- Update module github.com/docker/go-connections to v0.6.0 by @renovate[bot] in [#521](https://github.com/nicholas-fedor/watchtower/pull/521)
- Update dependency go to v1.24.6 by @renovate[bot] in [#513](https://github.com/nicholas-fedor/watchtower/pull/513)
- Update module github.com/prometheus/client_golang to v1.23.0 by @renovate[bot] in [#500](https://github.com/nicholas-fedor/watchtower/pull/500)
- Update module github.com/docker/docker to v28.3.3+incompatible by @renovate[bot] in [#495](https://github.com/nicholas-fedor/watchtower/pull/495)
- Update module github.com/docker/cli to v28.3.3+incompatible by @renovate[bot] in [#494](https://github.com/nicholas-fedor/watchtower/pull/494)
- Update module github.com/onsi/gomega to v1.38.0 by @renovate[bot] in [#473](https://github.com/nicholas-fedor/watchtower/pull/473)
- Update dependency go to v1.24.5 by @renovate[bot] in [#470](https://github.com/nicholas-fedor/watchtower/pull/470)
- Update go dependencies by @nicholas-fedor in [#429](https://github.com/nicholas-fedor/watchtower/pull/429)

### Fixed

- Enhance SMTP configuration with timeout constant and URL parameters by @nicholas-fedor in [#527](https://github.com/nicholas-fedor/watchtower/pull/527)
- Streamline StopContainer implementation by @nicholas-fedor in [#504](https://github.com/nicholas-fedor/watchtower/pull/504)

## [1.11.6] - 2025-07-16

### Chores

- Update module github.com/spf13/pflag to v1.0.7 by @renovate[bot] in [#407](https://github.com/nicholas-fedor/watchtower/pull/407)
- Update module golang.org/x/text to v0.27.0 by @renovate[bot] in [#384](https://github.com/nicholas-fedor/watchtower/pull/384)
- Update module github.com/docker/docker to v28.3.2+incompatible by @renovate[bot] in [#383](https://github.com/nicholas-fedor/watchtower/pull/383)
- Update module github.com/docker/cli to v28.3.2+incompatible by @renovate[bot] in [#379](https://github.com/nicholas-fedor/watchtower/pull/379)
- Update module github.com/docker/cli to v28.3.1+incompatible by @renovate[bot] in [#370](https://github.com/nicholas-fedor/watchtower/pull/370)
- Update module github.com/docker/docker to v28.3.1+incompatible by @renovate[bot] in [#371](https://github.com/nicholas-fedor/watchtower/pull/371)

### Fixed

- Restore proxy, DialContext, and redirect handling in NewAuthClient by @nicholas-fedor in [#403](https://github.com/nicholas-fedor/watchtower/pull/403)

## [1.11.5] - 2025-07-01

### Changed

- Enhance usage examples in doc.go by @nicholas-fedor in [#344](https://github.com/nicholas-fedor/watchtower/pull/344)

### Chores

- Update module github.com/docker/cli to v28.3.0+incompatible by @renovate[bot] in [#356](https://github.com/nicholas-fedor/watchtower/pull/356)
- Update module github.com/docker/docker to v28.3.0+incompatible by @renovate[bot] in [#357](https://github.com/nicholas-fedor/watchtower/pull/357)
- Update module github.com/nicholas-fedor/shoutrrr to v0.8.15 by @renovate[bot] in [#349](https://github.com/nicholas-fedor/watchtower/pull/349)
- Refactor `SliceSubtract` and update comment by @nicholas-fedor in [#343](https://github.com/nicholas-fedor/watchtower/pull/343)
- Update github.com/google/pprof by @nicholas-fedor in [#327](https://github.com/nicholas-fedor/watchtower/pull/327)

### Fixed

- Handle unauthenticated registries and update linting by @nicholas-fedor in [#369](https://github.com/nicholas-fedor/watchtower/pull/369)
- Fix registry redirect handling for image updates by @nicholas-fedor in [#359](https://github.com/nicholas-fedor/watchtower/pull/359)
- Resolve update failures of containers with multiple networks by @nicholas-fedor in [#351](https://github.com/nicholas-fedor/watchtower/pull/351)
- Ensure pinned container images are skipped during updates by @nicholas-fedor in [#347](https://github.com/nicholas-fedor/watchtower/pull/347)
- Increase default timeout to 30s and demote timeout log to debug by @nicholas-fedor in [#325](https://github.com/nicholas-fedor/watchtower/pull/325)
- Resolve premature image cleanup by @nicholas-fedor in [#321](https://github.com/nicholas-fedor/watchtower/pull/321)
- Demote log messages to debug by @nicholas-fedor in [#315](https://github.com/nicholas-fedor/watchtower/pull/315)

## [1.11.2] - 2025-06-10

### Fixed

- Reduce MAC address warning to debug for non-running containers by @nicholas-fedor in [#314](https://github.com/nicholas-fedor/watchtower/pull/314)
- Reintroduce option to skip tls verification by @nicholas-fedor in [#312](https://github.com/nicholas-fedor/watchtower/pull/312)

## [1.11.0] - 2025-06-07

### Chores

- Update module golang.org/x/text to v0.26.0 by @renovate[bot] in [#304](https://github.com/nicholas-fedor/watchtower/pull/304)
- Update module github.com/nicholas-fedor/shoutrrr to v0.8.14 by @renovate[bot] in [#297](https://github.com/nicholas-fedor/watchtower/pull/297)
- Update module github.com/docker/docker to v28.2.2+incompatible by @renovate[bot] in [#291](https://github.com/nicholas-fedor/watchtower/pull/291)
- Update module github.com/docker/cli to v28.2.2+incompatible by @renovate[bot] in [#290](https://github.com/nicholas-fedor/watchtower/pull/290)
- Update module github.com/nicholas-fedor/shoutrrr to v0.8.13 by @renovate[bot] in [#283](https://github.com/nicholas-fedor/watchtower/pull/283)
- Update module github.com/docker/docker to v28.2.1+incompatible by @renovate[bot] in [#287](https://github.com/nicholas-fedor/watchtower/pull/287)
- Update module github.com/docker/cli to v28.2.1+incompatible by @renovate[bot] in [#288](https://github.com/nicholas-fedor/watchtower/pull/288)
- Update module github.com/docker/cli to v28.2.0+incompatible by @renovate[bot] in [#286](https://github.com/nicholas-fedor/watchtower/pull/286)

### Fixed

- Resolve DOCKER_API_VERSION 404 errors and enhance API handling by @nicholas-fedor in [#305](https://github.com/nicholas-fedor/watchtower/pull/305)

## [1.10.0] - 2025-05-27

### Added

- Add linking and output messages by @piksel
- Add support for "none" scope by @piksel
- Add unit test for volume subpath preservation by @nicholas-fedor in [#265](https://github.com/nicholas-fedor/watchtower/pull/265)
- Add DisableMemorySwappiness flag for Podman compatibility by @nicholas-fedor in [#264](https://github.com/nicholas-fedor/watchtower/pull/264)

### Chores

- Bump github.com/prometheus/client_golang from 1.17.0 to 1.18.0 by @dependabot[bot]
- Bump github.com/spf13/viper from 1.18.1 to 1.18.2 by @dependabot[bot]
- Bump github.com/spf13/viper from 1.17.0 to 1.18.1 by @dependabot[bot]
- Bump go/stdlib to v1.20.x by @piksel
- Bump github.com/spf13/cobra from 1.7.0 to 1.8.0 by @dependabot[bot]
- Bump golang.org/x/text from 0.13.0 to 0.14.0 by @dependabot[bot]
- Bump github.com/docker/cli from 24.0.6+incompatible to 24.0.7+incompatible by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.28.1 to 1.29.0 by @dependabot[bot]
- Bump github.com/docker/docker from 24.0.6+incompatible to 24.0.7+incompatible by @dependabot[bot]
- Bump github.com/prometheus/client_golang by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.28.0 to 1.28.1 by @dependabot[bot]
- Replace usages of ioutil by @donuts-are-good
- Bump golang.org/x/net from 0.16.0 to 0.17.0 by @dependabot[bot]
- Bump golang.org/x/net from 0.15.0 to 0.16.0 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.27.10 to 1.28.0 by @dependabot[bot]
- Bump github.com/docker/distribution from 2.8.2+incompatible to 2.8.3+incompatible by @dependabot[bot]
- Update codecov/codecov-action digest to b203f00 by @renovate[bot] in [#258](https://github.com/nicholas-fedor/watchtower/pull/258)

### Fixed

- Correct logging level of watchtower handling for shutdown signals and context cancellation by @nicholas-fedor in [#282](https://github.com/nicholas-fedor/watchtower/pull/282)
- Instance cleanup without scope by @piksel
- Set nopull param from args by @piksel
- Handle missing healthcheck keys in config by @piksel
- Use new healthcheck config if not overridden by @piksel

### New Contributors

- @donuts-are-good made their first contribution

## [1.9.2] - 2025-05-08

### Chores

- Update Go version and dependencies by @nicholas-fedor in [#251](https://github.com/nicholas-fedor/watchtower/pull/251)
- Update module github.com/nicholas-fedor/shoutrrr to v0.8.9 by @renovate[bot] in [#248](https://github.com/nicholas-fedor/watchtower/pull/248)
- Update module golang.org/x/text to v0.25.0 by @renovate[bot] in [#244](https://github.com/nicholas-fedor/watchtower/pull/244)
- Update module github.com/docker/docker to v28.1.1+incompatible by @renovate[bot] in [#217](https://github.com/nicholas-fedor/watchtower/pull/217)
- Update module github.com/docker/cli to v28.1.1+incompatible by @renovate[bot] in [#215](https://github.com/nicholas-fedor/watchtower/pull/215)

### Fixed

- Enhance host networking and alias handling for container recreation by @nicholas-fedor in [#237](https://github.com/nicholas-fedor/watchtower/pull/237)

## [1.9.0] - 2025-04-14

### Changed

- Optional tag filter by @Foxite in [#205](https://github.com/nicholas-fedor/watchtower/pull/205)
- Enhance default-legacy template with fields and debug logging by @nicholas-fedor in [#200](https://github.com/nicholas-fedor/watchtower/pull/200)

### Chores

- Update module github.com/nicholas-fedor/shoutrrr to v0.8.8 by @renovate[bot] in [#203](https://github.com/nicholas-fedor/watchtower/pull/203)

## [1.8.8] - 2025-04-08

### Changed

- Update shoutrrr to v0.8.7 by @nicholas-fedor in [#194](https://github.com/nicholas-fedor/watchtower/pull/194)
- Change staleness check logging to debug by @nicholas-fedor in [#193](https://github.com/nicholas-fedor/watchtower/pull/193)
- Enhance HEAD request compatibility with OCI indexes by @nicholas-fedor in [#185](https://github.com/nicholas-fedor/watchtower/pull/185)
- Standardize comments by @nicholas-fedor in [#175](https://github.com/nicholas-fedor/watchtower/pull/175)
- Standardize logrus logging by @nicholas-fedor in [#171](https://github.com/nicholas-fedor/watchtower/pull/171)

### Chores

- Update module github.com/prometheus/client_golang to v1.22.0 by @renovate[bot] in [#184](https://github.com/nicholas-fedor/watchtower/pull/184)
- Update module github.com/onsi/ginkgo/v2 to v2.23.4 by @renovate[bot] in [#176](https://github.com/nicholas-fedor/watchtower/pull/176)
- Update module golang.org/x/text to v0.24.0 by @renovate[bot] in [#174](https://github.com/nicholas-fedor/watchtower/pull/174)

### Fixed

- Exclude Aliases and DNSNames on default bridge network by @nicholas-fedor in [#163](https://github.com/nicholas-fedor/watchtower/pull/163)

## [1.8.6] - 2025-03-31

### Added

- Add debug logging by @nicholas-fedor in [#141](https://github.com/nicholas-fedor/watchtower/pull/141)

### Changed

- Improve pre-1.44 api support by @nicholas-fedor in [#152](https://github.com/nicholas-fedor/watchtower/pull/152)
- Enhance network preservation and lifecycle management by @nicholas-fedor in [#128](https://github.com/nicholas-fedor/watchtower/pull/128)
- Revert "fix(container): preserve static MAC address in StartContainer with te…" by @nicholas-fedor in [#124](https://github.com/nicholas-fedor/watchtower/pull/124)

### Chores

- Update go deps by @nicholas-fedor in [#154](https://github.com/nicholas-fedor/watchtower/pull/154)
- Update module github.com/spf13/viper to v1.20.1 by @renovate[bot] in [#144](https://github.com/nicholas-fedor/watchtower/pull/144)
- Update module github.com/docker/docker to v28.0.4+incompatible by @renovate[bot] in [#140](https://github.com/nicholas-fedor/watchtower/pull/140)
- Update module github.com/docker/cli to v28.0.4+incompatible by @renovate[bot] in [#138](https://github.com/nicholas-fedor/watchtower/pull/138)
- Update module github.com/docker/docker to v28.0.3+incompatible by @renovate[bot] in [#139](https://github.com/nicholas-fedor/watchtower/pull/139)
- Update module github.com/onsi/gomega to v1.36.3 by @renovate[bot] in [#130](https://github.com/nicholas-fedor/watchtower/pull/130)
- Update module github.com/onsi/ginkgo/v2 to v2.23.3 by @renovate[bot] in [#129](https://github.com/nicholas-fedor/watchtower/pull/129)
- Update module github.com/onsi/ginkgo/v2 to v2.23.2 by @renovate[bot] in [#127](https://github.com/nicholas-fedor/watchtower/pull/127)
- Update module github.com/docker/docker to v28.0.2+incompatible by @renovate[bot] in [#121](https://github.com/nicholas-fedor/watchtower/pull/121)
- Update module github.com/onsi/ginkgo/v2 to v2.23.1 by @renovate[bot] in [#122](https://github.com/nicholas-fedor/watchtower/pull/122)
- Update module github.com/docker/cli to v28.0.2+incompatible by @renovate[bot] in [#120](https://github.com/nicholas-fedor/watchtower/pull/120)

### Fixed

- Update test to reflect updated shoutrrr teams handling by @nicholas-fedor in [#155](https://github.com/nicholas-fedor/watchtower/pull/155)
- Enhance RunHTTPServer shutdown handling by @nicholas-fedor in [#137](https://github.com/nicholas-fedor/watchtower/pull/137)
- Preserve static MAC address in StartContainer with test coverage by @nicholas-fedor in [#123](https://github.com/nicholas-fedor/watchtower/pull/123)

## [1.8.5] - 2025-03-19

### Chores

- Update module github.com/nicholas-fedor/shoutrrr to v0.8.5 by @renovate[bot] in [#118](https://github.com/nicholas-fedor/watchtower/pull/118)
- Merge pull request #106 from nicholas-fedor/renovate/github.com-spf13-viper-1.x by @nicholas-fedor in [#106](https://github.com/nicholas-fedor/watchtower/pull/106)
- Update module github.com/spf13/viper to v1.20.0 by @renovate[bot]

## [1.8.4] - 2025-03-14

### Changed

- Merge pull request #104 from nicholas-fedor/deps/package-updates by @nicholas-fedor in [#104](https://github.com/nicholas-fedor/watchtower/pull/104)
- Merge pull request #93 from nicholas-fedor/renovate/golang.org-x-net-0.x by @nicholas-fedor in [#93](https://github.com/nicholas-fedor/watchtower/pull/93)
- Merge pull request #92 from nicholas-fedor/renovate/github.com-onsi-ginkgo-v2-2.x by @nicholas-fedor in [#92](https://github.com/nicholas-fedor/watchtower/pull/92)
- Merge pull request #94 from nicholas-fedor/renovate/golang.org-x-text-0.x by @nicholas-fedor in [#94](https://github.com/nicholas-fedor/watchtower/pull/94)
- Merge pull request #89 from nicholas-fedor/renovate/go-1.x by @nicholas-fedor in [#89](https://github.com/nicholas-fedor/watchtower/pull/89)
- Merge pull request #91 from nicholas-fedor/renovate/golang.org-x-net-0.x by @nicholas-fedor in [#91](https://github.com/nicholas-fedor/watchtower/pull/91)
- Merge pull request #90 from nicholas-fedor/renovate/github.com-prometheus-client_golang-1.x by @nicholas-fedor in [#90](https://github.com/nicholas-fedor/watchtower/pull/90)
- Merge pull request #86 from nicholas-fedor/renovate/github.com-nicholas-fedor-shoutrrr-0.x by @nicholas-fedor in [#86](https://github.com/nicholas-fedor/watchtower/pull/86)

### Chores

- Update package dependencies by @nicholas-fedor
- Update digest module golang.org/x/net to v0.37.0 by @renovate[bot]
- Update digest module github.com/onsi/ginkgo/v2 to v2.23.0 by @renovate[bot]
- Update digest module golang.org/x/text to v0.23.0 by @renovate[bot]
- Update digest go to 1.24.1 by @renovate[bot]
- Update digest module golang.org/x/net to v0.36.0 by @renovate[bot]
- Update digest module github.com/prometheus/client_golang to v1.21.1 by @renovate[bot]
- Update digest module github.com/nicholas-fedor/shoutrrr to v0.8.3 by @renovate[bot]

## [1.8.3] - 2025-02-26

### Changed

- Merge pull request #85 from nicholas-fedor/switch-shoutrrr-from-containrrr-to-nicholas-fedor by @nicholas-fedor in [#85](https://github.com/nicholas-fedor/watchtower/pull/85)
- Merge pull request #83 from nicholas-fedor/renovate/github.com-docker-cli-28.x by @nicholas-fedor in [#83](https://github.com/nicholas-fedor/watchtower/pull/83)
- Merge pull request #84 from nicholas-fedor/renovate/github.com-docker-docker-28.x by @nicholas-fedor in [#84](https://github.com/nicholas-fedor/watchtower/pull/84)

### Chores

- Update digest module github.com/docker/cli to v28.0.1+incompatible by @renovate[bot]
- Update digest module github.com/docker/docker to v28.0.1+incompatible by @renovate[bot]

### Removed

- Remove references to containrrr for nicholas-fedor by @nicholas-fedor

## [1.8.2] - 2025-02-20

### Changed

- Merge pull request #81 from nicholas-fedor/renovate/github.com-docker-docker-28.x by @nicholas-fedor in [#81](https://github.com/nicholas-fedor/watchtower/pull/81)
- Update deprecated method call by @nicholas-fedor
- Refactor by @nicholas-fedor
- Merge pull request #80 from nicholas-fedor/renovate/github.com-docker-cli-28.x by @nicholas-fedor in [#80](https://github.com/nicholas-fedor/watchtower/pull/80)
- Merge pull request #79 from nicholas-fedor/renovate/github.com-prometheus-client_golang-1.x by @nicholas-fedor in [#79](https://github.com/nicholas-fedor/watchtower/pull/79)
- Merge pull request #77 from nicholas-fedor/renovate/github.com-spf13-cobra-1.x by @nicholas-fedor in [#77](https://github.com/nicholas-fedor/watchtower/pull/77)
- Merge pull request #74 from nicholas-fedor/73-failed-merge-pull-request-72-from-nicholas-fedorrenovategithubcom-spf13--44 by @nicholas-fedor in [#74](https://github.com/nicholas-fedor/watchtower/pull/74)
- Version updates by @nicholas-fedor

### Chores

- Update digest module github.com/docker/docker to v28.0.0+incompatible by @renovate[bot]
- Update digest module github.com/docker/cli to v28.0.0+incompatible by @renovate[bot]
- Update digest module github.com/prometheus/client_golang to v1.21.0 by @renovate[bot]
- Update digest module github.com/spf13/cobra to v1.9.1 by @renovate[bot]

## [1.8.1] - 2025-02-16

### Changed

- Merge pull request #72 from nicholas-fedor/renovate/github.com-spf13-cobra-1.x by @nicholas-fedor in [#72](https://github.com/nicholas-fedor/watchtower/pull/72)
- Merge pull request #70 from nicholas-fedor/renovate/cimg-go-1.x by @nicholas-fedor in [#70](https://github.com/nicholas-fedor/watchtower/pull/70)
- Fix spelling by @nicholas-fedor
- Replace dot imports with explicit package references by @nicholas-fedor
- Replace dot imports with explicit package references by @nicholas-fedor
- Replace dot imports with explicit package references by @nicholas-fedor
- Correct indentation by @nicholas-fedor
- Reorganize imports by @nicholas-fedor
- Replace dot imports with explicit package references by @nicholas-fedor

### Chores

- Update digest module github.com/spf13/cobra to v1.9.0 by @renovate[bot]
- Merge pull request #67 from nicholas-fedor/dependabot/go_modules/golang.org/x/net-0.35.0 by @nicholas-fedor in [#67](https://github.com/nicholas-fedor/watchtower/pull/67)
- Bump golang.org/x/net from 0.34.0 to 0.35.0 by @dependabot[bot]

## [1.8.0] - 2025-02-08

### Changed

- Merge pull request #65 from nicholas-fedor/64-watchtower-container-update-failure by @nicholas-fedor in [#65](https://github.com/nicholas-fedor/watchtower/pull/65)
- Re-enable test by @nicholas-fedor
- Update minimum supported Docker API version by @nicholas-fedor
- Merge pull request #60 from nicholas-fedor/renovate/golang.org-x-text-0.x by @nicholas-fedor in [#60](https://github.com/nicholas-fedor/watchtower/pull/60)

### Chores

- Update digest module golang.org/x/text to v0.22.0 by @renovate[bot]

## [1.7.12] - 2025-02-01

### Changed

- Merge pull request #37 from nicholas-fedor/renovate/github.com-onsi-ginkgo-2.x by @nicholas-fedor in [#37](https://github.com/nicholas-fedor/watchtower/pull/37)
- Correct package dependencies by @nicholas-fedor
- Migrate to Ginkgo v2 by @nicholas-fedor

### Chores

- Update module github.com/onsi/ginkgo to v2.22.2 by @renovate[bot]

## [1.7.11] - 2025-02-01

### Chores

- Merge pull request #44 from nicholas-fedor/renovate/github.com-spf13-pflag-1.x by @nicholas-fedor in [#44](https://github.com/nicholas-fedor/watchtower/pull/44)
- Update module github.com/spf13/pflag to v1.0.6 by @renovate[bot]
- Merge pull request #42 from nicholas-fedor/renovate/github.com-docker-cli-27.x by @nicholas-fedor in [#42](https://github.com/nicholas-fedor/watchtower/pull/42)
- Update module github.com/docker/cli to v27.5.1+incompatible by @renovate[bot]
- Merge pull request #43 from nicholas-fedor/renovate/github.com-docker-docker-27.x by @nicholas-fedor in [#43](https://github.com/nicholas-fedor/watchtower/pull/43)
- Update module github.com/docker/docker to v27.5.1+incompatible by @renovate[bot]

## [1.7.10] - 2025-01-20

### Added

- Add version retractions by @nicholas-fedor

### Changed

- Merge pull request #41 from nicholas-fedor/39-broken-pkggodev-versioning by @nicholas-fedor in [#41](https://github.com/nicholas-fedor/watchtower/pull/41)
- Modify to indirect by @nicholas-fedor
- Dependency updates by @nicholas-fedor

### Chores

- Merge pull request #36 from nicholas-fedor/renovate/github.com-onsi-ginkgo-2.x by @nicholas-fedor in [#36](https://github.com/nicholas-fedor/watchtower/pull/36)
- Update module github.com/onsi/ginkgo to v2.22.2 by @renovate[bot]
- Merge pull request #35 from nicholas-fedor/renovate/github.com-onsi-ginkgo-2.x by @nicholas-fedor in [#35](https://github.com/nicholas-fedor/watchtower/pull/35)
- Update module github.com/onsi/ginkgo to v2.22.2 by @renovate[bot]
- Merge pull request #34 from nicholas-fedor/renovate/github.com-onsi-ginkgo-2.x by @nicholas-fedor in [#34](https://github.com/nicholas-fedor/watchtower/pull/34)
- Update module github.com/onsi/ginkgo to v2.22.2 by @renovate[bot]
- Merge pull request #33 from nicholas-fedor/renovate/github.com-onsi-ginkgo-2.x by @nicholas-fedor in [#33](https://github.com/nicholas-fedor/watchtower/pull/33)
- Update module github.com/onsi/ginkgo to v2.22.2 by @renovate[bot]

## [1.7.2] - 2025-01-18

### Added

- Add template preview by @piksel
- Add --health-check command line switch by @bugficks
- Add a label take precedence argument by @jebabin
- Support container network mode by @schizo99
- Add no-pull label for containers by @gilbsgilbs
- Add json template by @piksel
- Add oci image index support by @piksel
- Add porcelain output by @piksel
- Support secrets for notification_url by @jlaska
- Add general notification delay by @lazou
- Add title field to template data by @piksel
- Support delayed sending by @piksel
- Add context fields to lifecycle events by @piksel
- Add WATCHTOWER_INCLUDE_RESTARTING env for include-restarting flag by @ilike2burnthing
- Add defered closer calls for the http clients by @simskij
- Add http head based digest comparison to avoid dockerhub rate limits by @simskij
- Adds scopeUID config to enable multiple instances of Watchtower by @victorcmoura
- Add string functions for lowercase, uppercase and capitalize to shoutrrr templates by @PssbleTrngle
- Adds the option to skip TLS verification for a Gotify instance by @tammert
- Add template support for shoutrrr notifications by @arnested
- Added --trace flag and new log.Trace() lines for sensitive information by @tammert
- Add ability to overrider depending containers with special label by @Saicheg
- Add shoutrrr.go by @mbrandau
- Add shoutrrr by @mbrandau
- Add timeout override for pre-update lifecycle hook by @simskij
- Add --no-startup-message flag
- #387 fix: add comments to pass linting by @simskij
- Add support for multiple email recipients by @simskij
- Added Mail Subject Tag to email.go by @simskij
- Add --revive-stopped flag to start stopped containers after an update by @zoispag
- Add pre/post update check lifecycle hooks
- Add optional email delay by @simskij
- Add docker api version parameter by @kaloyan-raev
- Add support for Gotify notifications by @lukapeschke

### Changed

- Dependency update by @nicholas-fedor
- Consolidated all post-fork updates including dependency bumps and workflow changes by @dependabot[bot]
- Add a flag/env to explicitly exclude containers by name by @rdamazio
- Allow logging output to use JSON formatter by @GridexX
- Update shoutrrr to v0.8 by @piksel
- Enabled loading http-api-token from file by @piksel
- Log removed/untagged images by @piksel
- Merge pull request #1548 from containrrr/dependabot/go_modules/github.com/onsi/gomega-1.26.0 by @dependabot[bot]
- Set default email client host by @piksel
- Update shoutrrr to v0.7 by @piksel
- Ignore removal error due to non-existing containers by @nothub
- Preparations for soft deprecation of legacy notification args by @piksel
- Allow log level to be set to any level by @matthewmcneely
- Regex container name filtering by @mateuszdrab
- Update shoutrrr to v0.6.1 by @piksel
- Update shoutrrr to v0.6.1 by @piksel
- Optional query parameter to update only containers of a specified image by @Foxite
- Bump shoutrrr to v0.5.3 by @piksel
- Bump vulnerable packages by @simskij
- Bump version of vulnerable dependencies by @piksel
- Improve HTTP API logging, honor no-startup-message by @jinnatar
- Post update time out by @patricegautier
- Improve session result logging by @piksel
- Use a more specific error type for no container info by @MorrisLaw
- Update dependencies (sane go.mod) by @piksel
- Update to v0.5 by @piksel
- Session report collection and report templates by @piksel
- Pre-update lifecycle hook
- Allow hostname override for notifiers by @nightah
- * feat: custom user agent by @piksel
- Allow running periodic updates with enabled HTTP API by @DasSkelett
- Check container config before update by @piksel
- Feat/head failure toggle by @simskij
- Update shoutrrr to v0.4.4 by @piksel
- Make head pull failure warning toggleable by @piksel
- Move token logs to trace by @simskij
- Use short image/container IDs in logs by @piksel
- Include additional info in startup by @piksel
- Update Shoutrrr to v0.4 by @piksel
- Fix notifications and old instance cleanup by @piksel
- Prometheus support by @simskij
- Cherrypick notification changes from #450 by @simskij
- Log based on registry known-support - reduce noise on notifications by @tkalus
- Revert "feat(config): swap viper and cobra for config " by @simskij
- Clean up scope builder and remove fmt print by @simskij
- Make sure all different ref formats are supported by @simskij
- Swap viper and cobra for config by @piksel
- Move secret value "credentials" to trace log by @piksel
- Actually fix it by @simskij
- Allow watchtower to update rebooting containers
- Monitor-only for individual containers by @dhet
- Disabling color through environment variables by @bugficks
- Rolling restart by @osheroff
- Skip updating containers where no local image info can be retrieved by @piksel
- Make sure all shoutrrr notifications are sent by @CedricFinance
- Warning if `WATCHTOWER_NO_PULL` and` WATCHTOWER_MONITOR_ONLY` are used simultaneously. by @m-sedl
- Lifecycle logs as Debug instead of Info by @MichaelSp
- Allows flags containing sensitive stuff to be passed as files by @tammert
- Image of running container no longer needed locally by @tammert
- Update shoutrrr to get latest and updated services by @arnested
- Comment out test that is incompatible with CircleCI by @simskij
- Bump minimum API version to 1.25 by @simskij
- Increases stopContainer timeout to 10min by @bopoh24
- Increases stopContainer timeout from 60 seconds to 10min by @victorcmoura
- Watchtower HTTP API based updates by @victorcmoura
- Merge branch 'master' into all-contributors/add-mbrandau by @simskij
- Merge pull request #470 from mbrandau/add-shoutrrr by @simskij
- Update shoutrrr by @mbrandau
- Reuse router by @mbrandau
- Use CreateSender instead of calling Send multiple times by @mbrandau
- Adjust flags by @mbrandau
- Merge pull request #480 from containrrr/feature/367 by @simskij
- Feature/367 fix: skip container if pre-update command fails by @simskij
- Merge pull request #477 from mbrandau/no-startup-message by @simskij
- Merge pull request #465 from lukwil/feature/443 by @simskij
- Fix according to remarks
- Merge pull request #455 from pagdot/patch-1 by @simskij
- Return on error after http.Post to gotify instance by @pagdot
- Merge pull request #418 from jsclayton/fix/retain-cmd by @simskij
- Merge branch 'master' into fix/retain-cmd by @simskij
- Merge pull request #448 from raymondelooff/bugfix/188 by @simskij
- Unset Hostname when NetworkMode is container by @raymondelooff
- Tidy up mod and sum files by @simskij
- Extract code from the container package by @simskij
- #387 fix: switch to image id map and add additional tests by @simskij
- Merge pull request #436 from containrrr/feature/multiple-email-recipients by @simskij
- Merge pull request #393 from mindrunner/master by @simskij
- Merge branch 'master' into master by @simskij
- Proper set implementation by @mindrunner
- Do not delete same image twice when cleaning up by @mindrunner
- Merge pull request #423 from zoispag/feature/413-change-initial-log-from-debug-to-info by @simskij
- #413 Change initial logging message from debug to info by @zoispag
- Sync by @zoispag
- Merge branch 'master' into all-contributors/add-zoispag by @simskij
- Don’t delete cmd when runtime entrypoint is different by @jsclayton
- Update flags.go by @8ear
- Update flags.go by @8ear
- Update email.go by @8ear
- Update email.go by @8ear
- Update email.go by @8ear
- Update email.go by @8ear
- Fix a small typo by @foosel
- Update check.go by @sixth
- Feat/lifecycle hooks by @simskij
- Split out more code into separate files by @simskij
- Move actions into internal by @simskij
- Move actions into pkg by @simskij
- Move container into pkg by @simskij
- Extract types and pkgs to new files by @simskij
- Re-apply based on new go flags package by @zoispag
- Switch urfave to cobra by @simskij
- Exclude markdown files from coverage analysis by @simskij
- Setup a working pipeline by @simskij

### Chores

- Bump github.com/docker/docker from 24.0.5+incompatible to 24.0.6+incompatible by @dependabot[bot]
- Bump github.com/docker/cli from 24.0.5+incompatible to 24.0.6+incompatible by @dependabot[bot]
- Bump golang.org/x/net from 0.14.0 to 0.15.0 by @dependabot[bot]
- Bump golang.org/x/text from 0.12.0 to 0.13.0 by @dependabot[bot]
- Bump golang.org/x/net from 0.12.0 to 0.14.0 by @dependabot[bot]
- Bump golang.org/x/text from 0.11.0 to 0.12.0 by @dependabot[bot]
- Bump github.com/docker/cli by @dependabot[bot]
- Bump github.com/docker/docker by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.27.8 to 1.27.10 by @dependabot[bot]
- Bump github.com/docker/cli from 24.0.2+incompatible to 24.0.4+incompatible by @dependabot[bot]
- Bump golang.org/x/net from 0.11.0 to 0.12.0 by @dependabot[bot]
- Bump github.com/docker/docker from 24.0.2+incompatible to 24.0.4+incompatible by @dependabot[bot]
- Bump golang.org/x/net from 0.10.0 to 0.11.0 by @dependabot[bot]
- Bump github.com/prometheus/client_golang from 1.15.1 to 1.16.0 by @dependabot[bot]
- Bump github.com/spf13/viper from 1.15.0 to 1.16.0 by @dependabot[bot]
- Bump github.com/sirupsen/logrus from 1.9.2 to 1.9.3 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.27.7 to 1.27.8 by @dependabot[bot]
- Bump golang.org/x/text from 0.9.0 to 0.10.0 by @dependabot[bot]
- Bump github.com/stretchr/testify from 1.8.3 to 1.8.4 by @dependabot[bot]
- Bump github.com/docker/docker from 23.0.6+incompatible to 24.0.2+incompatible by @dependabot[bot]
- Bump github.com/docker/cli from 24.0.1+incompatible to 24.0.2+incompatible by @dependabot[bot]
- Bump github.com/docker/cli by @dependabot[bot]
- Bump github.com/stretchr/testify from 1.8.2 to 1.8.3 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.27.6 to 1.27.7 by @dependabot[bot]
- Bump github.com/sirupsen/logrus from 1.9.0 to 1.9.2 by @dependabot[bot]
- Bump github.com/docker/distribution from 2.8.1+incompatible to 2.8.2+incompatible by @dependabot[bot]
- Bump github.com/prometheus/client_golang by @dependabot[bot]
- Bump github.com/docker/cli by @dependabot[bot]
- Bump golang.org/x/net from 0.9.0 to 0.10.0 by @dependabot[bot]
- Bump github.com/docker/docker by @dependabot[bot]
- Bump github.com/docker/cli by @dependabot[bot]
- Bump github.com/docker/docker from 23.0.4+incompatible to 23.0.5+incompatible by @dependabot[bot]
- Bump github.com/docker/docker from 23.0.3+incompatible to 23.0.4+incompatible by @dependabot[bot]
- Bump github.com/docker/cli from 23.0.3+incompatible to 23.0.4+incompatible by @dependabot[bot]
- Bump github.com/prometheus/client_golang by @dependabot[bot]
- Bump github.com/stretchr/testify from 1.8.1 to 1.8.2 by @dependabot[bot]
- Bump github.com/robfig/cron by @dependabot[bot]
- Bump github.com/spf13/cobra from 1.6.1 to 1.7.0 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.26.0 to 1.27.6 by @dependabot[bot]
- Bump golang.org/x/net from 0.5.0 to 0.9.0 by @dependabot[bot]
- Bump golang.org/x/text from 0.6.0 to 0.8.0 by @dependabot[bot]
- Bump github.com/docker/cli from 20.10.23+incompatible to 23.0.3+incompatible by @dependabot[bot]
- Bump github.com/docker/docker from 23.0.2+incompatible to 23.0.3+incompatible by @dependabot[bot]
- Bump docker/docker from 20.10.23+inc to 23.0.2+inc by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.25.0 to 1.26.0 by @dependabot[bot]
- Bump github.com/docker/cli from 20.10.22+incompatible to 20.10.23+incompatible by @dependabot[bot]
- Bump github.com/spf13/viper from 1.14.0 to 1.15.0 by @dependabot[bot]
- Bump github.com/docker/docker from 20.10.22+incompatible to 20.10.23+incompatible by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.24.2 to 1.25.0 by @dependabot[bot]
- Bump golang.org/x/net from 0.4.0 to 0.5.0 by @dependabot[bot]
- Bump golang.org/x/text from 0.5.0 to 0.6.0 by @dependabot[bot]
- Bump github.com/docker/docker by @dependabot[bot]
- Bump github.com/docker/cli by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.24.1 to 1.24.2 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.24.0 to 1.24.1 by @dependabot[bot]
- Bump golang.org/x/net from 0.3.0 to 0.4.0 by @dependabot[bot]
- Bump golang.org/x/net from 0.1.0 to 0.3.0 by @dependabot[bot]
- Bump golang.org/x/text from 0.4.0 to 0.5.0 by @dependabot[bot]
- Bump github.com/spf13/viper from 1.13.0 to 1.14.0 by @dependabot[bot]
- Bump github.com/spf13/cobra from 1.6.0 to 1.6.1 by @dependabot[bot]
- Bump github.com/prometheus/client_golang from 1.13.0 to 1.14.0 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.23.0 to 1.24.0 by @dependabot[bot]
- Bump github.com/docker/cli from 20.10.19+incompatible to 20.10.21+incompatible by @dependabot[bot]
- Bump github.com/docker/docker from 20.10.19+incompatible to 20.10.21+incompatible by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.22.1 to 1.23.0 by @dependabot[bot]
- Bulk update dependencies by @piksel
- Bump github.com/docker/cli from 20.10.18+incompatible to 20.10.19+incompatible by @dependabot[bot]
- Bump github.com/docker/docker from 20.10.18+incompatible to 20.10.19+incompatible by @dependabot[bot]
- Bump github.com/spf13/cobra from 1.5.0 to 1.6.0 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.21.1 to 1.22.1 by @dependabot[bot]
- Bump golang.org/x/text from 0.3.7 to 0.3.8 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.20.2 to 1.21.1 by @dependabot[bot]
- Bump github.com/spf13/viper from 1.12.0 to 1.13.0 by @dependabot[bot]
- Bump github.com/docker/cli from 20.10.17+incompatible to 20.10.18+incompatible by @dependabot[bot]
- Bump github.com/docker/docker from 20.10.17+incompatible to 20.10.18+incompatible by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.20.1 to 1.20.2 by @dependabot[bot]
- Bump github.com/prometheus/client_golang from 1.7.1 to 1.13.0 by @dependabot[bot]
- Update go version to 1.18 by @jauderho
- Bump github.com/spf13/viper from 1.6.3 to 1.12.0 by @dependabot[bot]
- Bump github.com/docker/distribution from 2.8.0+incompatible to 2.8.1+incompatible by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.20.0 to 1.20.1 by @dependabot[bot]
- Bump github.com/stretchr/testify from 1.6.1 to 1.8.0 by @dependabot[bot]
- Bump github.com/onsi/gomega from 1.10.3 to 1.20.0 by @dependabot[bot]
- Bump github.com/sirupsen/logrus from 1.8.1 to 1.9.0 by @dependabot[bot]
- Bump github.com/spf13/cobra from 1.4.0 to 1.5.0 by @dependabot[bot]
- Bump github.com/onsi/ginkgo from 1.14.2 to 1.16.5 by @dependabot[bot]
- Bump github.com/docker/distribution from 2.7.1+incompatible to 2.8.0+incompatible by @dependabot[bot]
- Bump shoutrrr and containrd by @piksel

### Fixed

- Only remove container id network aliases by @piksel
- Check flag/docs consistency by @piksel
- Received typo by @testwill
- Ensure temp files are cleaned up by @piksel
- Correctly set the delay from options by @Tentoe
- Empty out the aliases on recreation by @simskij
- Always use container interface by @piksel
- Image name parsing behavior by @Pwuts
- Remove logging of credentials by @piksel
- Ignore empty challenge fields by @piksel
- Always add missing slashes to link names by @piksel
- Update metrics from sessions started via API by @SamKirsch10
- Refactor/simplify container mock builders by @piksel
- Explicitly accept non-commands as root args by @piksel
- Detect schedule set from env by @piksel
- Include icon in slack legacy url by @Choromanski
- Gracefully skip pinned images by @piksel
- Testing for flag files on windows by @piksel
- Title customization by @piksel
- Correctly handle non-stale restarts by @piksel
- Move invalid token to log field by @piksel
- Add missing portmap when needed by @piksel
- Linked/depends-on container restarting by @piksel
- Return appropriate status for unauthorized requests by @hypnoglow
- Fixing flags usage text to first capital letter. by @dhiemaz
- Fully reset ghttp server by @piksel
- Container client tests refactor by @piksel
- Reduce test output noise by @piksel
- Refactor client tests by @piksel
- Default templates and logic by @piksel
- Check container image info for nil by @piksel
- Fix metrics api test stability by @piksel
- Use default http transport for head by @piksel
- Merge artifacts and broken shoutrrr tests by @piksel
- Fix depends on behavior and simplify some of its logic by @simskij
- Move notify URL to trace log by @piksel
- Don't panic on unconfigured notifier by @piksel
- Disallow log level 'trace' by @zoispag
- Set log level to debug for message about API token by @zoispag
- Fix manifest tag index in manifest.go by @piksel
- Fix linting issues by @simskij
- Fix cleanup for rolling updates by @piksel
- Fix typo by @rg9400
- Fix default interval to be the intended value by @piksel
- Fix erroneous poll interval change by @simskij
- Return nil imageinfo when retrieve fails by @piksel
- Fix fmt and vetting issues by @simskij
- Make shoutrrr init failure a fatal error by @piksel
- Display errors on init failure by @piksel
- Always use configured delay for notifications by @piksel
- Fix linting and formatting by @simskij
- Fix some errors and clean up
- Improve logging by @simskij
- Update mock client for tests by @simskij
- Fix #472 by @mbrandau
- Fix some var ref errors by @simskij
- Switch exit code for run once to 0 by @simskij
- Resolve merge issues by @simskij
- Remove linting issues by @simskij
- Remove unnecessary cronSet check by @simskij
- Fix port typing issue introduced in 998e805 by @simskij
- Fix linter errors by @simskij

### Removed

- Remove unused cross package dependency on mock api server by @piksel
- Removed all potential debug password prints, both plaintext and encoded by @tammert

### New Contributors

- @nicholas-fedor made their first contribution
- @GridexX made their first contribution
- @testwill made their first contribution
- @Tentoe made their first contribution
- @schizo99 made their first contribution
- @Pwuts made their first contribution
- @gilbsgilbs made their first contribution
- @SamKirsch10 made their first contribution
- @matthewmcneely made their first contribution
- @mateuszdrab made their first contribution
- @jlaska made their first contribution
- @Foxite made their first contribution
- @lazou made their first contribution
- @jinnatar made their first contribution
- @patricegautier made their first contribution
- @MorrisLaw made their first contribution
- @hypnoglow made their first contribution
- @dhiemaz made their first contribution
- @nightah made their first contribution
- @DasSkelett made their first contribution
- @ilike2burnthing made their first contribution
- @tkalus made their first contribution
- @rg9400 made their first contribution
- @dhet made their first contribution
- @osheroff made their first contribution
- @CedricFinance made their first contribution
- @m-sedl made their first contribution
- @MichaelSp made their first contribution
- @PssbleTrngle made their first contribution
- @arnested made their first contribution
- @bopoh24 made their first contribution
- @Saicheg made their first contribution
- @pagdot made their first contribution
- @raymondelooff made their first contribution
- @mindrunner made their first contribution
- @jsclayton made their first contribution
- @8ear made their first contribution
- @foosel made their first contribution
- @sixth made their first contribution
- @kaloyan-raev made their first contribution
- @lukapeschke made their first contribution

## Compare Releases

- [unreleased](https://github.com/nicholas-fedor/watchtower/compare/v1.12.1...HEAD)
- [1.12.1](https://github.com/nicholas-fedor/watchtower/compare/v1.11.8...v1.12.1)
- [1.11.8](https://github.com/nicholas-fedor/watchtower/compare/v1.11.6...v1.11.8)
- [1.11.6](https://github.com/nicholas-fedor/watchtower/compare/v1.11.5...v1.11.6)
- [1.11.5](https://github.com/nicholas-fedor/watchtower/compare/v1.11.2...v1.11.5)
- [1.11.2](https://github.com/nicholas-fedor/watchtower/compare/v1.11.0...v1.11.2)
- [1.11.0](https://github.com/nicholas-fedor/watchtower/compare/v1.10.0...v1.11.0)
- [1.10.0](https://github.com/nicholas-fedor/watchtower/compare/v1.9.2...v1.10.0)
- [1.9.2](https://github.com/nicholas-fedor/watchtower/compare/v1.9.0...v1.9.2)
- [1.9.0](https://github.com/nicholas-fedor/watchtower/compare/v1.8.8...v1.9.0)
- [1.8.8](https://github.com/nicholas-fedor/watchtower/compare/v1.8.6...v1.8.8)
- [1.8.6](https://github.com/nicholas-fedor/watchtower/compare/v1.8.5...v1.8.6)
- [1.8.5](https://github.com/nicholas-fedor/watchtower/compare/v1.8.4...v1.8.5)
- [1.8.4](https://github.com/nicholas-fedor/watchtower/compare/v1.8.3...v1.8.4)
- [1.8.3](https://github.com/nicholas-fedor/watchtower/compare/v1.8.2...v1.8.3)
- [1.8.2](https://github.com/nicholas-fedor/watchtower/compare/v1.8.1...v1.8.2)
- [1.8.1](https://github.com/nicholas-fedor/watchtower/compare/v1.8.0...v1.8.1)
- [1.8.0](https://github.com/nicholas-fedor/watchtower/compare/v1.7.12...v1.8.0)
- [1.7.12](https://github.com/nicholas-fedor/watchtower/compare/v1.7.11...v1.7.12)
- [1.7.11](https://github.com/nicholas-fedor/watchtower/compare/v1.7.10...v1.7.11)
- [1.7.10](https://github.com/nicholas-fedor/watchtower/compare/v1.7.2...v1.7.10)

<!-- generated by git-cliff -->
