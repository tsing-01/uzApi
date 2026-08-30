-- 激活码：管理员创建（生效日期 + 充值金额），用户登录后自行绑定。
-- 一码一人：绑定时 status 置为 used 并写入 used_by，金额充入用户余额；
-- 用户绑定新码时，旧码 status 置为 replaced。
--
-- user_login_ips：记录用户登录过的客户端 IP，同一用户最多 2 个不同 IP（应用层限制），
-- 超出的 IP 在登录时被拦截；用户绑定新的激活码时清空自己的记录。

CREATE TABLE IF NOT EXISTS activation_codes (
    id         BIGSERIAL PRIMARY KEY,
    code       VARCHAR(64) NOT NULL UNIQUE,
    amount     DECIMAL(20,8) NOT NULL DEFAULT 0,
    status     VARCHAR(20) NOT NULL DEFAULT 'unused',
    starts_at  TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    used_by    BIGINT REFERENCES users(id) ON DELETE SET NULL,
    used_at    TIMESTAMPTZ,
    notes      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS activationcode_status ON activation_codes (status);
CREATE INDEX IF NOT EXISTS activationcode_used_by ON activation_codes (used_by);
CREATE INDEX IF NOT EXISTS activationcode_expires_at ON activation_codes (expires_at);

CREATE TABLE IF NOT EXISTS user_login_ips (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip           VARCHAR(45) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS userloginip_user_id_ip ON user_login_ips (user_id, ip);
CREATE INDEX IF NOT EXISTS userloginip_user_id ON user_login_ips (user_id);
