-- 分组 Cyber 自动扣款：请求命中上游 cyber_policy 时，在用量费用之外按分组
-- 设置的金额扣款一次。默认关闭、金额 0，存量分组行为不变。
ALTER TABLE groups ADD COLUMN IF NOT EXISTS cyber_auto_charge_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE groups ADD COLUMN IF NOT EXISTS cyber_auto_charge_amount DECIMAL(20,8) NOT NULL DEFAULT 0;
