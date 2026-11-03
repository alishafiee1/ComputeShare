CREATE TABLE IF NOT EXISTS join_tokens (
  id TEXT PRIMARY KEY, token_hash BLOB NOT NULL, salt BLOB NOT NULL, label TEXT NOT NULL DEFAULT '',
  expires_at INTEGER, max_uses INTEGER, uses INTEGER NOT NULL DEFAULT 0, revoked_at INTEGER, created_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS nodes (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE, device_pubkey TEXT NOT NULL,
  host_key TEXT NOT NULL, host_key_sig TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'offline',
  os TEXT, arch TEXT, specs_json TEXT, limits_json TEXT, approval INTEGER NOT NULL DEFAULT 0, version TEXT,
  last_seen INTEGER NOT NULL DEFAULT 0, online_since INTEGER NOT NULL DEFAULT 0, registered_at INTEGER NOT NULL, token_id TEXT);
CREATE TABLE IF NOT EXISTS stats_samples (
  node_id TEXT NOT NULL, ts INTEGER NOT NULL, cpu_pct REAL, ram_pct REAL, gpu_pct REAL, vram_pct REAL, net_rx_bps REAL, net_tx_bps REAL);
CREATE INDEX IF NOT EXISTS stats_node_ts ON stats_samples(node_id, ts);
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT, node_id TEXT NOT NULL, via TEXT NOT NULL, src_ip TEXT NOT NULL,
  started_at INTEGER NOT NULL, ended_at INTEGER, bytes_up INTEGER NOT NULL DEFAULT 0, bytes_down INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS bans (
  kind TEXT NOT NULL, value TEXT NOT NULL, until INTEGER, reason TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL,
  PRIMARY KEY(kind, value));
CREATE TABLE IF NOT EXISTS admins (
  username TEXT PRIMARY KEY, pw_hash BLOB NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS panel_sessions (
  id_hash BLOB PRIMARY KEY, username TEXT NOT NULL, csrf TEXT NOT NULL, ip TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, actor TEXT NOT NULL, ip TEXT NOT NULL, action TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '');
