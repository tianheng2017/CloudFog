-- 2026-09-08 热路径索引补漏（全系统性能审计发现）：
-- 1) billing_ledger.request_id：LedgerExistsByRequestID 按 request_id 查询**任意类型**，
--    此前只有 settle/refund 两个部分唯一索引可用 → 全表扫描；reserve:reclaim 每 15min
--    对每条过期冻结各调用一次，随只追加的 ledger 增长线性恶化。
-- 2) channel_groups(group_id)：PK 为 (channel_id, group_id)，按分组反查渠道前缀不匹配 → 全表扫描
--    （渠道可见性/管理端按分组筛选均为热路径）。
-- 3) users lower(email)/lower(username)：登录按 lower() 比对，而唯一索引大小写敏感 → 全表扫描
--    （登录是热路径且是暴力破解面；库内新数据已统一小写存储，函数索引对存量大小写亦正确）。
CREATE INDEX IF NOT EXISTS idx_ledger_request_id ON billing_ledger (request_id) WHERE request_id <> '';
CREATE INDEX IF NOT EXISTS idx_channel_groups_group_id ON channel_groups (group_id);
CREATE INDEX IF NOT EXISTS idx_users_email_lower ON users (lower(email)) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_users_username_lower ON users (lower(username)) WHERE deleted_at IS NULL;
