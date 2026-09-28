ALTER TABLE announcements ADD COLUMN IF NOT EXISTS is_pinned BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS announcements_single_pinned
    ON announcements (is_pinned) WHERE is_pinned = TRUE;
