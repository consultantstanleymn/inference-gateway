# Security gate

This file describes the security checks of the gateway. It lists what each job checks, how to run it on a laptop, and what is proven.

## Jobs in `.github/workflows/security.yml`

| Job | What it checks | Fail rule | Local command |
|---|---|---|---|
| `codeql` | Static analysis of the Go code. | Any error-level result. | None. CodeQL needs GitHub Actions. |
| `secrets` | gitleaks over the full git history. | Any finding. | `gitleaks git --config .gitleaks.toml --redact --no-banner --exit-code 1 .` |
| `sast` | golangci-lint with the `gosec` linter on. | Any issue. | `golangci-lint run ./...` |
| `deps` | `govulncheck` for Go and `pip-audit` for the training lock file. | A finding that is not in `security/exceptions.json`. | `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` and `pip-audit` (see below) |
| `image` | Trivy scan of the gateway image. | HIGH or CRITICAL with a fix. | `trivy image --severity HIGH,CRITICAL --ignore-unfixed --exit-code 1 localhost/inference-gateway:dev` |
| `sbom` | CycloneDX SBOM of the source tree. | Job error. | `trivy fs --format cyclonedx --output docs/engagement/evidence/sbom-source.cdx.json .` |
| `exceptions` | Checks `security/exceptions.json`. | A missing field or an expired date. | `python3 scripts/check_security_exceptions.py security/exceptions.json` |

The training lock file pins `torch` with the local tag `+cpu`. `pip-audit` cannot resolve that tag. The command removes `+cpu` first, then runs `pip-audit --no-deps --disable-pip`.

## Exceptions

An accepted finding goes into `security/exceptions.json`. Each item has an id, a tool, a reason, an owner, an expiry date (90 days at most) and a review note. An item that has not expired does not fail the job.

Four items are open today. Three are for `setuptools` 78.1.0 in the training lock file. The package is used on a developer machine to train models. It is not in the gateway image. One is for `libssl3` in the distroless base image: the gateway does not use DTLS, and the fixed package is not in the pinned base yet. The review date is 2027-02-07.

## Dependency fix

The first image scan found `golang.org/x/net` v0.60.0 (HIGH, fixed in 0.62.0). The module is now at v0.62.0 in `go.mod`. The tests pass. The image must be rebuilt to carry the fix.

## Code findings

The first run of `gosec` found 8 issues. Four are in test files. The test files start the built binary and copy fixtures with paths that the tests build. They are excluded in `.golangci.yml`. Four are in production code: two in `cmd/probe` and two in `internal/model`. Each has a `nolint` comment that says why. The probe is an operator tool. The model directory comes from operator configuration, not from a request.

## Status

| Check | Status |
|---|---|
| gitleaks, history and tree | Ran on this laptop. No findings. |
| golangci-lint with gosec | Ran on this laptop. 0 issues. |
| govulncheck | Ran on this laptop. 0 reachable vulnerabilities. |
| pip-audit | Ran on this laptop. 5 findings in `setuptools`, all in the exceptions file. |
| Trivy image scan | Ran on this laptop. Two HIGH findings: one fixed in `go.mod`, one filed as an exception. The image is rescanned after the next rebuild. |
| CycloneDX SBOM | See `evidence/sbom-source.cdx.json`. |
| `security.yml` workflow | Authored, not run. It passes `actionlint`. It never ran on GitHub. |
| CodeQL | Authored, not run. It needs GitHub Actions. |

Action versions are unverified until a run.
