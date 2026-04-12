CREATE TABLE IF NOT EXISTS submission_credentials (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  expires_at TEXT,
  allowed_sender_domains TEXT,
  allowed_sender_addresses TEXT,
  description TEXT,
  last_auth_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO submission_credentials(
  username,
  password_hash,
  enabled,
  allowed_sender_domains,
  allowed_sender_addresses,
  description
) VALUES
  ('alice@example.com', '4e738ca5563c06cfd0018299933d58db1dd8bf97f6973dc99bf6cdc64b5550bd', 1, 'example.com', 'billing@example.net', 'tutorial user'),
  ('ops@example.com', '4e738ca5563c06cfd0018299933d58db1dd8bf97f6973dc99bf6cdc64b5550bd', 1, 'other.example', '', 'ops user');
