# Media and financial review for v0.2.147

## Confirmed fixes

- Custom account image tests recognize supported image families before and after
  model mapping. Explicit image mode supports arbitrary aliases. Tests send one
  request to `/images/generations`, omit unsupported `response_format`, accept
  base64 or public image URLs, bound responses and redact upstream error secrets.
- Model plaza video prices retain the configured per-second unit, resolution
  tier overrides and independent video multiplier. Explicit per-request prices
  remain per-request prices; model names do not override the billing mode.
- Multipart reference URL arrays are admitted without relaxing uniqueness of
  paid scalar fields. Scalar file/array aliases are rejected. Reference video
  array aliases retain their surcharge in billing metadata.
- Video duration/count fields reject ambiguous or invalid numeric forms before
  precharge and forwarding. Conflicting `duration` and `seconds` are rejected.
  String integer durations and native Sora multipart defaults agree between
  precharge and usage settlement.
- Unknown payment order types cannot bypass disabled balance recharge or its
  multiplier. Both order creation and fulfillment reject unknown/crossed types.
- Payment fulfillment codes use secure random values. The public redemption
  entry rejects the reserved `PAY-` namespace, including legacy codes. Recovery
  checks the code's type, amount and redeemed user before completing an order.
- Non-forced refunds require the full available balance atomically at execution.
  Pending gateway refunds retain the already-deducted balance. Success does not
  deduct it twice; explicit failure restores it with the order status transition
  in one transaction. Unknown/query-error outcomes keep the hold. Pending refunds
  cannot be resubmitted as new refunds. Finalization also checks the queried
  order version so a delayed result from an earlier attempt cannot affect a
  later refund or restore its balance.
- Redeem-code batch edits lock rows before checking status. Redemption checks
  expiry atomically and reads benefits after claiming the code, preventing stale
  benefit reads or restoration of an already-used code through concurrent edits.
- Balance repository mutations reject non-finite values; deduction APIs reject
  negative amounts. This is defense in depth, not a claim that public endpoints
  previously accepted caller-supplied balance mutations.
- Removed the unused `@lobehub/icons` package and its transitive dependency tree.
  Application icons already use local static assets. Production dependency audit
  reports zero known advisories after removal.
- Updated Vite, Vitest and vulnerable development transitive dependencies.
  The remaining version-based advisory is GHSA-vfj7-8cjw-p6xm in development-only
  `braces@3.0.3`, for which no upstream fixed release is available. A committed
  pnpm patch bounds parser and recursive AST depth; a CI check exercises normal
  glob patterns and rejects deeply nested strings and supplied ASTs. The audit
  still reports this original package version despite the local mitigation.
  The development server binds to loopback by default.

## Reviewed boundaries

The review covered user/admin routing, current-role JWT validation, admin API
keys, user ownership of usage and media tasks, media prepayment capabilities,
reservation/refund idempotency, payment notifications and fulfillment, redemption,
and workbench reference handling. No user-facing usage-log write or arbitrary
balance-credit endpoint was found. Payment callbacks retain signature, amount,
merchant/provider snapshot and order-state checks. Media prepayment privileges
are server context values, not client headers.

Default, Sora and 8yes are the supported Custom media protocols. The retired
adapter and its UI option remain absent; an unsupported stored protocol is
rejected. Image/chat compatibility remains independent of video endpoint choice.

## Validation

Release validation includes backend unit tests, PostgreSQL integration tests,
Go lint, frontend lint/typecheck/build, all 2,081 frontend tests in 270 files,
and production dependency audit. Regression cases cover malformed paid fields,
reference surcharges, refund double application/concurrent spending, redemption
ownership/expiry/edit races, and order-type policy bypasses.

Browser checks exercised workbench reference submission, authenticated image
preview, HTML preview rejection, and protocol create/edit controls at desktop,
390-pixel and 320-pixel widths. No page errors or horizontal overflow occurred.

## Operational limits

- Upstream behavior was exercised with local/mock providers. No real paid 8yes
  or affected Custom provider request was made. A provider-side 502 may still
  require its exact model, credentials/group permissions and upstream logs.
- Legacy pending refunds that had already released credit can recover the full
  refunded credit on confirmation, potentially leaving a negative user balance.
  Providers without refund-query support still require manual reconciliation;
  their pending holds cannot be automatically finalized through the query API.
- The changes do not automatically repair historical stolen/redeemed internal
  payment codes. A mismatched code owner or amount now stops fulfillment for
  investigation instead of marking the order completed.
- Passing these checks is evidence for the reviewed paths, not proof that every
  possible vulnerability in the project has been eliminated.
