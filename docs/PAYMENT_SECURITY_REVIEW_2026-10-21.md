# Payment Security Review (2026-10-21)

## Scope and conclusion

Reviewed the EasyPay notification signature replay fix in upstream release
[v0.2.14](https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.14), issue
[#7881](https://github.com/Wei-Shaw/sub2api/issues/7881), and implementation PR
[#7888](https://github.com/Wei-Shaw/sub2api/pull/7888), merged as
`d97ccc952ff3b7f63b44c45079ee6a28422f1393`.

The local implementation already blocks the reported order-signature replay
path. Client return URLs lose their query and fragment, and must target the
internal payment result path on an allowed host. Notifications reject fields
outside the standard allowlist, including empty unknown fields, as well as
duplicate fields. Required notification fields, MD5 signature type, signature,
and exact merchant identity are checked before fulfillment. Fulfillment checks
the order/provider binding, paid amount, and order state.

Additional defense added in this release: oversized callback bodies and query
strings return HTTP 413. Previously the POST reader truncated at the size limit,
which could ignore trailing fields. This change enforces validation of the whole
accepted notification; the truncation alone did not demonstrate forged credit.

Regression coverage exercises GET and POST callbacks, order-signature replay
rejection, nonstandard and duplicate fields, merchant and amount mismatches,
nonfinite/invalid amounts, oversized messages, and repeated completed-order
notifications. Rejected notifications leave the order and balance unchanged.
These results cover the reported vulnerability and reviewed paths; they are not
a claim that all payment providers or future changes are free of vulnerabilities.

## Exclusive recharge authorization

- `payment_exclusive_enabled` controls enforcement without rewriting grants.
- `users.exclusive_recharge_enabled` defaults to false; migration 220 adds the
  column and a partial index for the authorized-user list.
- Administrator configuration and authorization management keep their existing
  administrator checks. An administrator's own recharge uses the same grant
  check as other users.
- All authenticated payment APIs enforce the current database grant, independent
  of cached login claims. Order creation and public order recovery also check
  authorization. Provider webhooks remain available to settle genuine existing
  payments after a user loses permission.
- The frontend checks access on entry, hides purchase/order navigation, and exits
  an open payment page on revocation. A long poll wakes immediately after a
  same-process grant or master-switch change; a three-second database fallback
  observes changes processed by another instance. Hidden tabs stop watching and
  refresh when visible. Every backend operation checks authorization regardless
  of browser refresh timing.
- The administration panel uses existing controls, an email filter, fixed
  ten-row pages, and page correction after removal. The user edit switch saves
  independently so saving unrelated profile edits cannot restore an old grant.

The browser checks use isolated mocked API fixtures. Server authorization and
database behavior are covered separately by Go tests; no real provider charge
or production account was used for verification.
