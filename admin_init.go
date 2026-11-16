package main

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/bcrypt"
)

// defaultDatabasePath 返回当前目录下的默认数据库文件路径。
func defaultDatabasePath() string {
	workingDir, err := os.Getwd()
	if err != nil {
		return filepath.Join(".", "team-battle.sqlite")
	}
	return filepath.Join(workingDir, "team-battle.sqlite")
}

// runInit 创建首个超级管理员，并只在终端输出一次随机初始密码。
func runInit(args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	dbPath := flags.String("db", "", "SQLite database path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	path := *dbPath
	if path == "" {
		path = defaultDatabasePath()
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		return err
	}
	if err := migrate(db); err != nil {
		return err
	}

	var existingID int64
	err = db.QueryRow(`SELECT id FROM users WHERE role=? ORDER BY id LIMIT 1`, roleSuperAdmin).Scan(&existingID)
	if err == nil {
		return fmt.Errorf("超级管理员已存在，ID=%d；为避免意外覆盖，未重置密码", existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	password, err := randomAdminPassword(24)
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	result, err := db.Exec(`INSERT INTO users(username, display_name, password_hash, role) VALUES(?,?,?,?)`, "admin", "超级管理员", string(hash), roleSuperAdmin)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	fmt.Printf("超级管理员初始化成功\n账号：admin\nID：%d\n随机密码：%s\n", id, password)
	return nil
}

func randomAdminPassword(length int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	data := make([]byte, length)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	password := make([]byte, length)
	for i, value := range data {
		password[i] = alphabet[int(value)%len(alphabet)]
	}
	return string(password), nil
}
