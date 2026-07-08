-- 添加视频按秒计费的价格字段到 pricing_intervals 表
-- mizaawa 修改版本：支持视频按秒计费模式

ALTER TABLE pricing_intervals
ADD COLUMN IF NOT EXISTS video_per_sec_price NUMERIC(20, 10);

COMMENT ON COLUMN pricing_intervals.video_per_sec_price IS '视频模式：每秒价格（USD），用于视频按秒计费';