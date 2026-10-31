CREATE TABLE IF NOT EXISTS launchboard_ideas (
 id SERIAL PRIMARY KEY,
 seed_key TEXT UNIQUE,
 title VARCHAR(100) NOT NULL,
 description VARCHAR(600) NOT NULL,
 category TEXT NOT NULL CHECK (category IN ('Product', 'Design', 'Engineering')),
 status TEXT NOT NULL DEFAULT 'idea' CHECK (status IN ('idea', 'building', 'shipped')),
 author VARCHAR(30) NOT NULL,
 seed_votes INTEGER NOT NULL DEFAULT 0,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS launchboard_votes (
 idea_id INTEGER NOT NULL REFERENCES launchboard_ideas(id) ON DELETE CASCADE,
 visitor TEXT NOT NULL,
 PRIMARY KEY (idea_id, visitor)
);

ALTER TABLE launchboard_ideas ADD COLUMN IF NOT EXISTS assignee VARCHAR(60) NOT NULL DEFAULT '';
ALTER TABLE launchboard_ideas ADD COLUMN IF NOT EXISTS priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('high','normal','low'));
ALTER TABLE launchboard_ideas ADD COLUMN IF NOT EXISTS due_date DATE;
ALTER TABLE launchboard_ideas ADD COLUMN IF NOT EXISTS archived BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE launchboard_ideas ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE launchboard_ideas ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE launchboard_ideas ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE launchboard_ideas ALTER COLUMN description TYPE VARCHAR(3000);
CREATE INDEX IF NOT EXISTS launchboard_ideas_due ON launchboard_ideas(due_date) WHERE NOT archived AND status <> 'shipped';
