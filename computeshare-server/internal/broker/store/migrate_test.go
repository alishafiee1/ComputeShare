package store

import (
	"database/sql"
	_ "embed"
	"path/filepath"
	"testing"
)

//go:embed testdata/v1_schema.sql
var v1Schema string

func TestMigrateV1DropsPort(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(v1Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nodes(id,name,name_key,device_pubkey,host_key,host_key_sig,port,status,registered_at) VALUES('n1','myPc','mypc','dk','hk','sig',40000,'offline',1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if has, _ := hasColumn(s.db, "nodes", "port"); has {
		t.Fatal("port column still present")
	}
	var name string
	if err := s.db.QueryRow(`SELECT name FROM nodes WHERE id='n1'`).Scan(&name); err != nil || name != "myPc" {
		t.Fatalf("node lost: %v %q", err, name)
	}
	if ok, _ := s.NameAvailable("MYPC"); ok {
		t.Fatal("name should still be taken after migration")
	}
	// second open is a no-op
	s.Close()
	if s2, err := Open(p); err != nil {
		t.Fatal(err)
	} else {
		s2.Close()
	}
}
