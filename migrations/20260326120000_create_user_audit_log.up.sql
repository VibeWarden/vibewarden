-- Create the user_audit_log table.
-- This table is append-only: records are never updated or deleted.
CREATE TABLE user_audit_log (
    id        UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id   UUID         NOT NULL,
    action    VARCHAR(50)  NOT NULL,
    actor_id  VARCHAR(255),
    timestamp TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    metadata  JSONB
);

CREATE INDEX idx_audit_user_id  ON user_audit_log(user_id);
CREATE INDEX idx_audit_timestamp ON user_audit_log(timestamp);
CREATE INDEX idx_audit_actor_id  ON user_audit_log(actor_id) WHERE actor_id IS NOT NULL;
