package app

import (
	"database/sql"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestEnsureDefaultAdmin(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "empty.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	if err := ensureDefaultAdmin(db); err != nil {
		t.Fatal(err)
	}
	var username, role, passwordHash string
	if err := db.QueryRow(`SELECT username,role,password_hash FROM users`).Scan(&username, &role, &passwordHash); err != nil {
		t.Fatal(err)
	}
	if username != "admin" || role != roleSuperAdmin {
		t.Fatalf("默认账号不正确: username=%q role=%q", username, role)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte("test1234")); err != nil {
		t.Fatalf("默认密码不正确: %v", err)
	}
	if err := ensureDefaultAdmin(db); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("重复启动创建了额外账号: %d", count)
	}
}
