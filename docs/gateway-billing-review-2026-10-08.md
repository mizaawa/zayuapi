# Gateway and Billing Review, 2026-10-08

## Scope

Reviewed checkout `8ce252a2b` and the recent changes in `1658843c7`,
`1c7294d3d`, `30bb6006d`, `9fa6e1880`, `cd0f42048`, and `75a6f6797`.
After fetching origin, the review also includes the v0.2.124 security changes
in `409355e5d` and their test-fixture correction in `e48fc9831`. Those commits
are retained in the merged history.
The review covers usage validation, billing settlement, protocol conversion,
stream failures, group failover, deleted API keys, payment callbacks, admin
bootstrap, custom system prompts, simulated monitoring, API key limits,
session revocation, and step-up authentication.

## Flex and Priority Pricing

Keep the existing Flex multiplier of 0.5 and Priority fallback multiplier of 2.
Explicit Priority prices, when configured, still take precedence over the
fallback multiplier. These rules were introduced by upstream commit
[`87f4ed591e`](https://github.com/Wei-Shaw/sub2api/commit/87f4ed591e9f779c3c19f6d49c129466c93f78fc)
in March 2026, before the recent local audit fixes. Flex is a separate processing
tier; it is not a parameter that cancels a duplicate charge.

OpenAI's [Flex documentation](https://developers.openai.com/api/docs/guides/flex-processing)
uses Batch API pricing. Its
[Responses API reference](https://developers.openai.com/api/reference/resources/responses/methods/create)
also says the returned tier can differ from the requested tier. Neither fact
alone establishes that every provider's tier echo is authoritative.

Checked upstream main at `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`:

- [Billing resolution](https://github.com/Wei-Shaw/sub2api/blob/3f1a2ea0a760730e3bc528105c00b4ee4f23e469/backend/internal/service/service_tier_billing.go)
  uses the final outbound request tier and only accepts a response tier to lower
  the bill. A Flex request is not raised to normal pricing by a `default` echo.
- [Resolution tests](https://github.com/Wei-Shaw/sub2api/blob/3f1a2ea0a760730e3bc528105c00b4ee4f23e469/backend/internal/service/service_tier_billing_test.go)
  explicitly cover `flex never raised to default`.
- Codex OAuth has an explicit exception: a `default` response echo does not
  cancel effective Fast pricing. Upstream commit
  [`fdf9751c12`](https://github.com/Wei-Shaw/sub2api/commit/fdf9751c1223a74a7153e537c6d9d1fb14ee9cad)
  documents that distinction.

Adding `service_tier: flex` to a request that is forwarded and accepted as Flex
and observing half the base charge therefore does not demonstrate a new billing
bypass. This patch does not change pricing multipliers or replace the billing
contract with unconditional response-echo pricing.

## Reproduced Findings and Fix

### Response-only billing regression in v0.2.124

Commit `409355e5d` made `RecordUsage` bill exclusively from
`UpstreamServiceTier`. Missing response metadata therefore removed the Flex
discount, a Codex `default` echo removed effective Fast pricing, and a more
expensive response tier could raise the charge. These behaviors conflict with
the upstream contract above.

Settlement now resolves the final outbound tier against the observed response
tier. The response may lower the charge, but cannot raise it; an absent or
unknown tier preserves the outbound tier. For OpenAI OAuth and setup-token
credentials, a `default` echo is non-authoritative. Shadow accounts resolve
their credential account before this decision. Response observation is retained,
and the usage log stores the tier actually used for settlement.

`TestOpenAIRecordUsageServiceTierMatchesUpstreamContract` covers 21 cases,
including missing and unknown echoes, API-key downgrades, prevention of upward
repricing, normalization, OAuth and setup-token exceptions, and other OAuth
providers. It checks total cost, actual cost, debit, and recorded tier.
The 24 partial-usage handler cases also cover Priority requests with and without
a response echo across Responses, passthrough, Chat Completions, and Messages.

### Outbound tier after policy and protocol conversion

Five compatibility forwarders used a stale or discarded tier for billing.
The result now takes its tier from the request body after policy application
and protocol conversion.

| Path | Reproduced behavior before the fix | Corrected behavior |
| --- | --- | --- |
| Chat Completions to Responses, including Responses-shaped input | Filtered Priority still charged at Priority; Flex forced to Priority still charged at Flex | Bill the resulting outbound tier |
| Messages to Responses | Filtered Fast/Priority still charged at Priority | Bill the resulting outbound tier |
| Raw Chat Completions | Filtered or forced tiers still billed from the original body | Bill the resulting outbound tier |
| Responses to raw Chat Completions | A nonempty original tier survived subsequent policy changes in billing metadata | Bill the resulting outbound tier |
| Messages to raw Chat Completions | An extra `service_tier: flex` field was discarded during conversion but still discounted the charge | A discarded field no longer affects the charge |

For the regression fixture (GPT-5.4, 1,000 input and 100 output tokens, rate
multiplier 1), normal pricing is $0.004. A filtered Priority request must cost
$0.004, a request forced to Priority must cost $0.008, and a supported Flex
request must retain $0.002 pricing. A discarded Messages-body Flex field must
cost $0.004.

`TestOpenAICompatBillingUsesOutboundServiceTier` covers 54 cases across six
input/routing combinations, each with streaming and nonstreaming responses.
It verifies the captured upstream body, forward result, recorded service tier,
`actual_cost`, and debit passed to the user repository. Before the final three
raw-route fixes, 22 cases reproduced the remaining errors.

## Other Reviewed Changes

No additional reproducible regression requiring a code change was identified
in the reviewed scope:

- Usage parsers reject negative, fractional, nonfinite, and overflowing values;
  pricing and protocol bridges keep their existing validation boundaries.
- Group failover rechecks group access and billing eligibility. A stream that
  has committed output is not replayed as a new request; retained partial usage
  still flows into settlement and existing deduplication.
- A deleted API key only skips counters for the missing key. User/subscription
  settlement and billing deduplication remain transactional; other errors still
  roll back.
- EasyPay callbacks reject unrecognized signed parameters, and payment return
  URLs discard preexisting query parameters. Admin bootstrap generates random
  credentials and enforces the bcrypt input limit consistently with login.
- Managed monitoring requests require the expected owner and signed proof;
  proof headers are removed before upstream forwarding. Simulation returns
  before any real upstream request.
- Custom system prompts retain ownership checks, size limits, structured body
  rewriting, and usage reported by the upstream for billing.
- WebDAV redirect authentication restrictions show no regression in the
  reviewed code.
- Billing deduplication no longer trusts caller-supplied tracing IDs. Server
  request identities remain stable across a request's settlement path. Queue
  overflow and shutdown both fall back to bounded synchronous billing.
- Gemini and OpenAI Messages retain observed usage after stream read failures
  or client disconnects. Existing committed-output guards prevent replay.
- API key quota and rate limits reject negative and nonfinite values; key
  deletion and status changes no longer clear custom-key conflict throttles.
- Access-token revocation atomically increments a persisted database version.
  JWT middleware reads the latest user version from the repository, and refresh
  tokens also verify that version. Password changes still invalidate the
  credential fingerprint. Step-up grants for legacy tokens bind to the bearer
  credential; settings failures do not disable the gate.
- Payment notifications validate merchant identity and currency using the
  order snapshot or a uniquely bound configured provider. Missing or ambiguous
  legacy identity fails closed. Refund queries use the gateway refund amount.

This is a source and regression-test review, not a production penetration test
or a guarantee that the repository has no other vulnerabilities.

## Local Validation

Validation ran on Windows with Go 1.26.6 and pnpm 9.15.9. The table below records
checks completed for the compatibility-route changes before merging v0.2.124;
the final merged commit is also subject to the release gate described below.

| Check | Result |
| --- | --- |
| Full Go unit suite (`go test -p 1 -tags=unit ./...`) | Passed |
| Integration-tag suite (`go test -p 1 -tags=integration -json ./...`) | No failed events; database suite skipped because Docker is unavailable |
| Outbound-tier regression | All 54 cases passed in the integration-tag test log; focused rerun after test-only lint cleanup also passed |
| golangci-lint 2.9.0 | Passed, 0 issues |
| Frontend lint and typecheck | Passed |
| Critical frontend Vitest plus monitor form coverage | 12 files, 204 tests passed |
| Frontend production build | Passed with a 4 GB local Node heap limit |
| Backend build with embedded frontend (`go build -p 1 -tags=embed ./cmd/server`) | Passed |
| govulncheck | No reachable vulnerabilities reported |
| Production pnpm audit and exception validation | No high/critical advisories; 13 moderate and 2 low advisories remain |
| Portable deployment script checks | Syntax, admin bootstrap, private GitHub install/startup, Docker runtime/security, and Caddy cache checks passed |
| Apple container lifecycle check | Cannot validate on Windows: requires macOS BSD `stat` and file permissions |

Database integration and Apple deployment checks must run on the configured
Linux/macOS GitHub Actions runners. Existing local GitHub credentials have been
verified with repository push access. Publication is gated on successful CI and
Security Scan runs for the final pushed commit, followed by a full release build.

After merging v0.2.124, the corrected partial-usage handler cases passed locally.
The full unit run initially hit the existing wall-clock SQL timing assertion;
its isolated rerun passed without a production or test-code change. Final remote
check results are linked from the release notes.
