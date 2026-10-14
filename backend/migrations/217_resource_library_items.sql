-- Administrator-preserved copies of image/video workbench results. Rows and
-- their files outlive workbench retention so content can be reviewed later.
CREATE TABLE IF NOT EXISTS resource_library_items (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('image', 'video')),
    source_task_id TEXT NOT NULL,
    source_index INTEGER NOT NULL DEFAULT 0 CHECK (source_index >= 0),
    name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    sha256 TEXT NOT NULL,
    user_id BIGINT NOT NULL,
    user_email TEXT NOT NULL DEFAULT '',
    api_key_id BIGINT NOT NULL DEFAULT 0,
    group_id BIGINT NOT NULL DEFAULT 0,
    group_name TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    billing_id TEXT NOT NULL DEFAULT '',
    cost NUMERIC(20,8),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    generated_at TIMESTAMPTZ NOT NULL,
    source_expires_at TIMESTAMPTZ,
    saved_by BIGINT NOT NULL DEFAULT 0,
    saved_by_email TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT resource_library_items_source_key UNIQUE (source_task_id, source_index)
);

CREATE INDEX IF NOT EXISTS resource_library_items_created_idx ON resource_library_items (created_at DESC);