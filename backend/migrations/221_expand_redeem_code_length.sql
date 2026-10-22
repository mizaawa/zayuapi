-- Payment fulfillment codes use PAY- plus a 35-character random redeem code.
-- Widen the shared redeem-code column so existing paid orders can be retried.
ALTER TABLE redeem_codes
    ALTER COLUMN code TYPE VARCHAR(64);
