package app

import (
	"database/sql"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

// defaultDatabasePath 返回当前目录 runtime 下的默认数据库文件路径。
func defaultDatabasePath() string {
	workingDir, err := os.Getwd()
	if err != nil {
		return filepath.Join(".", "runtime", "team-battle.sqlite")
	}
	return filepath.Join(workingDir, "runtime", "team-battle.sqlite")
}

// ensureDefaultAdmin 为全新数据库创建默认超级管理员，已有账号时不覆盖任何数据。
func ensureDefaultAdmin(db *sql.DB) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("test1234"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO users(username,display_name,password_hash,role) VALUES('admin','超级管理员',?,?)`, string(hash), roleSuperAdmin)
	return err
}
