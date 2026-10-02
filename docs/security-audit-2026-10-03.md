# Backend security audit — 2026-10-03

Reviewed base commit: `fee09d0743cb0b624a63a333c029c48a58c21f9d`.
Assessment date: 2026-10-03, Asia/Vientiane (UTC+07:00).
Status: **SEC-01, SEC-02 and SEC-03 patched in the working tree**. The original
assessment and reproductions below describe the reviewed base commit. Remediation
changes and validation are recorded separately here.

## Remediation — 2026-10-03

Patch base: `17a316cd9c62d23be16ec5a3649e6cc563b7ea9c`.
The patch is uncommitted and has not been deployed.

- **SEC-01:** OTP request returns a secret 256-bit `challenge` in both production
  and development. PostgreSQL stores only its SHA-256 hash. Verification requires
  phone, code and the matching newest challenge before inspecting or changing
  the failed-attempt budget. Missing/guessed/older challenges cannot exhaust an
  existing issuance. The five-failure ceiling, newest-only rule, five-minute
  lifetime and atomic one-time consumption/phone-key insertion are preserved.
  Verification also limits 30 requests/IP/minute, 15/IP and User-Agent/minute,
  and 10/challenge/five minutes. Redis failures reject verification before writes.
  The IP/device limits can still affect customers sharing an IP/device signature;
  the secret challenge prevents remote callers from spending the OTP's own budget.
- **SEC-02:** Select the legacy owner principal before account limiting. All
  non-email legacy aliases share a five-attempt/ten-minute quota, independent of
  the submitted alias or source IP. The ten-attempt IP limit and named staff email
  limits remain active. Legacy owner login remains optional.
- **SEC-03:** Replace Gin request-dumping recovery with an outer recovery handler
  covering middleware and routes. Logs contain only request ID, route and status;
  panic values, headers, query values and bodies are never formatted. A started
  response is aborted without appending an error document. Regressions cover
  ordinary/abort/nil panics, broken pipes, connection resets and started responses.
- **Dependencies:** Upgrade `pgx/v5` to `v5.9.2`, `quic-go` to `v0.59.1`,
  `x/crypto` to `v0.56.0`, `x/image` to `v0.45.0`, `x/net` to `v0.57.0`,
  `x/text` to `v0.41.0` and the required `x/sync` to `v0.22.0`.
  Official `govulncheck` v1.8.0 with `-tags nomsgpack -show verbose ./...`
  exits 0 with zero affected symbols and zero affected imported packages.
  One module-only advisory remains: [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932)
  covers unmaintained `x/crypto/openpgp`, has no fixed version, and none of its
  affected packages is imported by this backend. `x/crypto` is needed for scrypt;
  OpenPGP must not be added as an authentication/encryption dependency.
- **CI:** Add `make security`, with pinned `govulncheck` installed in the Docker
  development tools image. Push, pull request, manual and weekly scheduled runs
  check dependencies. Symbol findings fail the gate; conditional findings require
  documented review according to [operations](operations.md).

Migration [00004](../migrations/00004_otp_challenge.sql) adds `challenge_hash` and
expires outstanding issuances. Run it before the new API, deploy all API replicas
and coordinate the storefront change: retain `data.challenge` from `/otp/request`
and send it alongside phone and code to `/otp/verify`. Existing codes must be
requested again. OpenAPI, API guide/reference, Postman examples, README and feature
parity document this contract. No account or customer cookie was introduced.

The original vulnerability proof assertions have been inverted into security
regressions. Database coverage now includes unbound attempt isolation, resends
and concurrent one-time verification; unit coverage includes expiry, replay,
production response secrecy and limiter failures.

Patch validation on 2026-10-03 (Asia/Vientiane):

- `make check`: passed formatting, vet and the complete unit/regression suite.
- `make integration`: passed against guarded `shopnext_test` and isolated Redis
  with `-race` (11.207s); migration 00004 applied successfully.
- `make security`: passed in the Docker tools image (exit 0), with zero symbol
  and imported-package findings and the one module-only OpenPGP advisory above.
- Targeted application/HTTP/domain/config `-race` checks: passed.
- `make build` (`docker compose --env-file .env.example build`): passed for the
  Alpine runtime containing API, worker, migration and staff binaries.
- Generated API reference/Postman `--check` and `git diff --check`: passed.

The remaining sections preserve the original assessment as historical evidence;
its source line numbers and scanner results refer to the original base commit.

## Scope and evidence

Reviewed the Go/Gin HTTP API, OTP and staff identity, customer ownership,
orders/payments/refunds, PostgreSQL queries and transactions, Redis limiting,
media storage, outbound provider clients, configuration, Docker and CI.
Local attack probes used synthetic requests and records. Database-backed probes
ran exclusively through the guarded `shopnext_test` integration harness and
isolated test Redis. No production host, real merchant/SMS service, or real
customer records were tested. `.env` was not read.

This is an application assessment, not evidence that a deployed system has
been compromised. Production configuration, reverse-proxy/WAF controls,
network exposure, host security, and native-library CVEs remain outside the
validated scope. Absence of a finding does not establish absence of a flaw.

## Confirmed findings

### SEC-01 — Unauthenticated callers can exhaust another customer's OTP

**Severity:** Medium. **Category:** authentication availability / abuse.
**Location:** `internal/application/identity.go:106–138` (`VerifyOTP`),
`internal/adapters/http/router.go:278–300` (public OTP verification route).

The request supplies only a phone number and code. `VerifyOTP` locks and updates
the newest OTP for that phone, increments its shared attempt count on a wrong
code, and refuses all verification after five attempts. Neither this route nor
the use case applies a verification traffic limiter or binds the OTP to the
requesting flow. The ordinary CSRF header is public and provides no caller identity.

**Local reproduction:** issue a synthetic OTP, submit five wrong six-digit
codes without any verification key or login, then submit the correct code.
The correct code receives HTTP 429 (`OTP_ATTEMPTS_EXCEEDED`), no phone key is
issued, and the stored attempt count is five. This was reproduced through the
actual HTTP router and PostgreSQL persistence.

**Impact and prerequisites:** an unauthenticated caller who knows a customer's
phone can block that customer's active verification and repeat the attack
against new OTPs. This blocks new checkout authorization and OTP-based access
to private order details; it does not revoke already-issued phone/trust keys
or prove account takeover. Sustained blocking requires guessing during each
OTP's active window. Edge throttles may reduce the attack volume.

**Recommended fix:** return an unpredictable issuance challenge from OTP request
and require it during verification. Bind attempt accounting to that challenge
while preserving newest-OTP-only behavior and the failed-attempt ceiling.
Add IP/device/challenge throttles and abuse monitoring. An IP throttle alone
does not stop a caller from spending the victim's five shared attempts.
An implementation that changes the OTP API must update OpenAPI and feature parity.

**Proofs:**

- `tests/integration/security_business_test.go:13` — HTTP/DB reproduction.
- `internal/application/security_identity_test.go:86` — demonstrates that
  verification performs no limiter check.

### SEC-02 — Legacy owner login aliases bypass the account guessing limit

**Severity:** Medium, conditional on legacy login being enabled.
**Category:** authentication rate-limit bypass.
**Location:** `internal/application/identity.go:181–218` (`Login`).

Login applies the account quota to the submitted `email` value. When legacy
fallback is configured, every value without `@` checks the same shared owner
password. Different aliases therefore receive different account counters even
though they target one principal.

**Local reproduction:** five wrong attempts with one alias reach password
verification; the sixth is rate-limited. A new non-email alias on the same
synthetic IP reaches password verification again. Changing both alias and
synthetic source IP restarts both counters.

**Impact and prerequisites:** an attacker can bypass the five-attempt account
limit for the shared owner password. The ten-attempt IP limit still works;
distributed source IPs are required to bypass it too. An owner session still
requires guessing the correct password. No password bypass was demonstrated.
Named staff email quotas are unaffected by this specific alias issue.

**Recommended fix:** select the authentication principal before rate limiting
and use one fixed legacy-principal quota for every legacy alias. Prefer named
staff accounts and disable optional shared-owner fallback when it is unnecessary.
Retain IP throttling alongside the principal quota.

**Proof:** `internal/application/security_identity_test.go:105`.

For prioritization, DREAD is used only as a judgment aid, not a CVSS score.
Scores are Damage/Reproducibility/Exploitability/Affected users/Discoverability:
SEC-01 `4/8/8/4/5` (5.8); SEC-02 `8/8/2/3/5` (5.2). The latter has serious
potential impact but still requires password guessing and optional configuration.

## Conditional logging weakness

**SEC-03 — Low / production trigger unproven.**
`internal/adapters/http/router.go:44` installs `gin.CustomRecovery` with Gin's
default error writer. For an `EPIPE`, `ECONNRESET`, or `http.ErrAbortHandler`
panic, Gin dumps request headers and masks only `Authorization`. The custom
phone-verification and trusted-device headers are not masked.

`internal/adapters/http/security_audit_test.go:160` injects an abort panic in a
test-only route and captures the output in memory. Synthetic customer markers
appear; the staff authorization marker is redacted. Captured values are never
printed. No attacker-triggerable production panic path was identified, so this
is not a demonstrated remote credential theft.

Replace request-dumping recovery with a handler that logs only request ID,
route and status. Avoid logging the panic value if it can contain private data.
If real customer keys reach logs, a reader could replay a still-valid key.

## Dependency advisory triage

Official `govulncheck` v1.8.0 with `-tags nomsgpack ./...` returned exit 3 and
three symbol-level findings. Its call graph is conservative; the scanner's
"affected" classification alone does not establish runtime exploitability.

| Advisory | Pinned version → minimum fixed version | Application evidence |
| --- | --- | --- |
| [GO-2026-5970](https://pkg.go.dev/vuln/GO-2026-5970) | `golang.org/x/text v0.38.0` → `v0.39.0` | Invalid UTF-8 can stall `norm.Iter`. A bounded library probe reproduced the iterator timeout. Scanner-reported `Form` operations completed on the same input; no public input reaching `Iter` was identified. Backend exploitation remains unproven. |
| [GO-2026-5676](https://pkg.go.dev/vuln/GO-2026-5676) | `quic-go v0.59.0` → `v0.59.1` | Requires HTTP/3. The API uses `net/http.Server.ListenAndServe`, never Gin `RunQUIC`; provider clients configure no HTTP/3 transport. The reviewed runtime lacks the prerequisite. |
| [GO-2026-5004](https://pkg.go.dev/vuln/GO-2026-5004) | `pgx/v5 v5.7.5` → `v5.9.2` | Requires non-default simple protocol and a dollar-quoted SQL literal containing an attacker-controlled placeholder. The driver defaults to extended protocol; no matching runtime SQL was found. A deployment DSN can override protocol mode and was not inspected. |

Update these dependencies in a focused change, then rerun the scanner and
regression suite. The scan also reported two package-level and thirteen
module-level advisories without affected symbol calls; those are not thirteen
demonstrated application exploits. `govulncheck` does not comprehensively audit
the C code embedded by `chai2010/webp` or the container operating system.

`gosec` v2.29.0 scanned 29 production Go files (5,579 lines), with no package
loading errors, and returned four alerts (exit 1). Manual triage found:

| Alert | Location | Disposition |
| --- | --- | --- |
| G115: unsigned-to-signed conversion | `postgres/store.go:72` | The 64-bit FNV result becomes PostgreSQL's signed 64-bit advisory-lock key. This preserves the bit pattern and does not weaken the lock or overflow a monetary value. No exploit found. |
| G101: password in default URL | `platform/config.go:22` | A documented local-development database default, not an exposed production secret. Production operators must supply independent database credentials; configuration does not reject this development DSN in production. Deployment exposure was not inspected. |
| G304: variable file path | `media/storage.go:96` | The filename is generated internally from an encoded-content hash and random hex suffix, not an untrusted path. The storage root comes from operator configuration; traversal probes were rejected. |
| G302: file permissions exceed 0600 | `media/storage.go:96` | Uploaded catalog images use 0640 under a 0750 directory and are deliberately served publicly. This does not establish sensitive-file exposure. |

CI currently runs vet/tests, but has no `govulncheck` or security-scanner gate.
Add dependency scanning and an explicit review process for conditional results
so future advisories do not depend on manual audits.

## Controls exercised

Local probes confirmed rejection of missing/forged/oversized staff bearer
tokens across protected administrative routes, invalid payment signatures,
oversized callbacks, untrusted forwarded IP headers, foreign origins, missing
CSRF headers, extra JSON fields/documents, and oversized JSON bodies.
Media traversal and symlink probes failed to disclose temporary files.

Database-backed probes confirmed server-owned prices, bounded quantities,
cross-phone payment-retry denial without replacing the owner's QR,
callback reference/amount validation, catalog SQL/wildcard handling,
draft-product filtering and cross-product variant containment. Existing tests
also exercise session revocation and transactional payment/refund/stock behavior.
These checks cover the tested cases; they are not exhaustive proofs.

Public pickup codes are required by this project's contract and are not an
authorization finding. Documented manual payment confirmation, unpaid COD
inventory consumption, and buyer-supplied branch creation are business trust
decisions requiring operational abuse controls, not demonstrated privilege bypasses.

## Verification and reproduction

The added audit files contain twelve test functions plus route/input subtests.
At the original assessment, some deliberately asserted vulnerable behavior to
preserve audit evidence. Their passing status did not mean SEC-01/02/03 were
fixed. The remediation above has inverted those assertions into regression
expectations.

```sh
make check
make integration
GOCACHE=/tmp/shopnext-security-go-cache go test -tags nomsgpack -race ./internal/application ./internal/adapters/http ./internal/domain ./internal/platform
govulncheck -tags nomsgpack ./...
gosec -tags nomsgpack ./...
```

Final validation:

- `make check`: passed formatting, vet and the test suite with all added files.
- `make integration`: passed all database-backed tests with `-race` (11.492s).
  The harness verifies `current_database() == shopnext_test` before any reset.
- Targeted application/HTTP/domain/config race checks: passed.
- `git diff --check`: passed.
- `govulncheck`: completed, exit 3; three symbol findings triaged above.
- `gosec`: completed, exit 1; four alerts triaged above, no loading errors.

An initial check ran while the HTTP proof file was being completed and failed
on a test-only route-inspection type assertion. The final concrete Gin-engine
assertion and the completed suite passed; no production fix was involved.

## Skills discovered

The find-skills workflow checked the leaderboard, searched the registry,
and inspected source/repository information. The Go security skill and its
checklist/threat-model references were applied through source review; no
permanent skill installation was made. Counts below are approximate registry
observations on the assessment date, not guarantees of quality.

| Skill and source | Use | Installs / repository stars | Install command |
| --- | --- | --- | --- |
| [golang-security — samber/cc-skills-golang](https://www.skills.sh/samber/cc-skills-golang/golang-security) | Go vulnerability hunting and security testing; used in this audit | ~41K / ~3.4K | `npx skills add samber/cc-skills-golang@golang-security` |
| [security-best-practices — openai/skills](https://www.skills.sh/openai/skills/security-best-practices) | Official backend secure-development and review guidance | ~9.9K / ~27.8K | `npx skills add openai/skills@security-best-practices` |
| [security-threat-model — openai/skills](https://www.skills.sh/openai/skills/security-threat-model) | Repository-specific cybersecurity threat modeling | ~6.9K / ~27.8K | `npx skills add openai/skills@security-threat-model` |

The literal `cybersecurity` CLI search returned no matches; the narrower
security search found the official threat-modeling option above.
