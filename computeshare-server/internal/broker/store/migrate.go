package store

import (
	"database/sql"
	"fmt"
)

const v1CopyCols = `id,name,name_key,device_pubkey,host_key,host_key_sig,status,os,arch,specs_json,limits_json,approval,version,last_seen,online_since,registered_at,token_id`

// migrate upgrades a v1 database in place: v1 nodes had a per-node "port INTEGER NOT NULL UNIQUE"
// column, which v2 removed (single SSH port). SQLite cannot DROP a UNIQUE column, so the table is rebuilt.
func migrate(db *sql.DB) error {
	has, err := hasColumn(db, "nodes", "port")
	if err != nil || !has {
		return err
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer db.Exec(`PRAGMA foreign_keys=ON`)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{
		`CREATE TABLE nodes_v2 (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE, device_pubkey TEXT NOT NULL,
  host_key TEXT NOT NULL, host_key_sig TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'offline',
  os TEXT, arch TEXT, specs_json TEXT, limits_json TEXT, approval INTEGER NOT NULL DEFAULT 0, version TEXT,
  last_seen INTEGER NOT NULL DEFAULT 0, online_since INTEGER NOT NULL DEFAULT 0, registered_at INTEGER NOT NULL, token_id TEXT)`,
		`INSERT INTO nodes_v2(` + v1CopyCols + `) SELECT ` + v1CopyCols + ` FROM nodes`,
		`DROP TABLE nodes`,
		`ALTER TABLE nodes_v2 RENAME TO nodes`,
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("migrate v1->v2: %w", err)
		}
	}
	return tx.Commit()
}

func hasColumn(db *sql.DB, table, col string) (bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return false, err
		}
		if n == col {
			return true, nil
		}
	}
	return false, rows.Err()
}
