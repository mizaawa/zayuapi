-- 只读候选筛查，适用于已启用统一账本的标准计费部署。不会补扣或改写数据。
-- 默认最近 24 小时，留出 5 分钟异步写入窗口；保留周期、免费价格、旧版直扣均需人工核对。
BEGIN READ ONLY;
SET LOCAL statement_timeout = '30s';

WITH candidates AS (
    SELECT u.created_at, u.request_id, u.api_key_id, u.model, u.inbound_endpoint,
           u.input_tokens, u.output_tokens, u.cache_read_tokens, u.cache_creation_tokens, u.actual_cost,
           CASE
               WHEN u.actual_cost > 0
                AND NOT EXISTS (SELECT 1 FROM usage_billing_dedup d
                                WHERE (d.request_id, d.api_key_id) = (u.request_id, u.api_key_id))
                AND NOT EXISTS (SELECT 1 FROM usage_billing_dedup_archive a
                                WHERE (a.request_id, a.api_key_id) = (u.request_id, u.api_key_id))
                   THEN 'positive_cost_without_dedup_receipt'
               WHEN u.actual_cost = 0 AND GREATEST(u.input_tokens, u.output_tokens,
                        u.cache_read_tokens, u.cache_creation_tokens) > 0 THEN 'metered_but_zero_cost'
               WHEN u.actual_cost = 0 AND u.inbound_endpoint IN
                    ('/v1/messages', '/v1/chat/completions', '/v1/embeddings', '/v1/responses', '/v1/responses/compact')
                    THEN 'text_request_without_metered_usage'
           END AS review_reason
    FROM usage_logs u
    JOIN api_keys k ON k.id = u.api_key_id
    WHERE u.created_at >= now() - interval '24 hours' AND u.created_at < now() - interval '5 minutes'
      AND u.billing_type = 0 AND u.rate_multiplier > 0 AND k.purpose <> 'channel_monitor'
      AND u.image_count = 0 AND COALESCE(u.billing_mode, 'token') = 'token'
      AND u.request_type <> 4 -- 策略拒绝有单独的收费开关，不当作普通生成
)
SELECT * FROM candidates WHERE review_reason IS NOT NULL ORDER BY created_at DESC LIMIT 500;

ROLLBACK;
