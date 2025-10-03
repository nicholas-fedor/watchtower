
## [Unreleased]

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

## Compare Releases

- [unreleased](https://github.com/nicholas-fedor/watchtower/compare/v1.12.0...HEAD)

<!-- generated by git-cliff -->
