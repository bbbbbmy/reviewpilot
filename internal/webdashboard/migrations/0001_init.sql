-- 0001_init.sql — initial schema for the web-dashboard SQLite store.
-- Mirrors design.md §P5D2.

CREATE TABLE IF NOT EXISTS repo (
  id                      INTEGER PRIMARY KEY AUTOINCREMENT,
  full_name               TEXT UNIQUE NOT NULL,
  provider                TEXT NOT NULL,
  model                   TEXT NOT NULL,
  max_input_tokens_per_pr  INTEGER NOT NULL DEFAULT 100000,
  feedback_window_days    INTEGER NOT NULL DEFAULT 30,
  enabled                 INTEGER NOT NULL DEFAULT 1,
  created_at              DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS review (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  repo_id       INTEGER NOT NULL REFERENCES repo(id),
  pr_number     INTEGER NOT NULL,
  commit_sha    TEXT NOT NULL,
  status        TEXT NOT NULL,
  reason        TEXT,
  tokens_in     INTEGER NOT NULL DEFAULT 0,
  tokens_out    INTEGER NOT NULL DEFAULT 0,
  started_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_review_repo_started
  ON review(repo_id, started_at DESC);

CREATE TABLE IF NOT EXISTS comment (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  review_id             INTEGER NOT NULL REFERENCES review(id),
  file_path             TEXT NOT NULL,
  line_number           INTEGER NOT NULL DEFAULT 0,
  body                  TEXT NOT NULL,
  fingerprint           TEXT NOT NULL,
  posted_to_platform    INTEGER NOT NULL DEFAULT 0,
  posted_to_platform_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_comment_review
  ON comment(review_id);

CREATE INDEX IF NOT EXISTS idx_comment_fingerprint
  ON comment(fingerprint);

CREATE TABLE IF NOT EXISTS feedback (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  comment_id  INTEGER NOT NULL REFERENCES comment(id),
  rating      TEXT NOT NULL,
  text        TEXT,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_feedback_comment
  ON feedback(comment_id);