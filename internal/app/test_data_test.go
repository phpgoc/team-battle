package app

import (
	"bytes"
	"database/sql"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestDemoImagesAreDetailedLocalAssets(t *testing.T) {
	// 防止演示数据退回单色占位或简易像素图片。
	for _, category := range testCategories {
		data, err := demoFS.ReadFile("testdata/demo/" + category.asset + ".png")
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width < 64 || config.Height < 64 || len(data) < 4096 {
			t.Fatalf("分类 %s 的演示图标不完整: %dx%d, %d 字节, %v", category.name, config.Width, config.Height, len(data), err)
		}
	}
	for _, username := range []string{"admin", "2", "3", "4", "5", "6", "7", "8", "9", "10"} {
		data, err := demoAvatar(username)
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || config.Width != 256 || config.Height != 256 || len(data) < 4096 {
			t.Fatalf("账号 %s 的演示头像不完整: %dx%d, %d 字节, %v", username, config.Width, config.Height, len(data), err)
		}
	}
}

func TestDemoDataHasConsistentCompleteMatches(t *testing.T) {
	f := newWorkflowFixture(t)
	if err := seedTestData(f.app.db); err != nil {
		t.Fatal(err)
	}
	// 再次初始化不覆盖已有比赛和账号。
	if err := seedTestData(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 10, `SELECT COUNT(*) FROM users WHERE username IN ('admin','2','3','4','5','6','7','8','9','10')`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM users WHERE username='admin' AND role='superadmin' AND display_name='超级管理员'`)
	f.assertCount(t, 9, `SELECT COUNT(*) FROM users WHERE username GLOB '[2-9]' OR username='10'`)
	f.assertCount(t, 4, `SELECT COUNT(*) FROM categories`)
	f.assertCount(t, 2, `SELECT COUNT(*) FROM backgrounds WHERE id IN (1,2) AND image_mime='image/png' AND length(image_data)=0 AND image_size>0`)
	f.assertCount(t, 5, `SELECT COUNT(*) FROM events WHERE status='completed'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='特殊对战1' AND id IN (SELECT event_id FROM event_players GROUP BY event_id HAVING COUNT(*)=8)`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='特殊对战2' AND id IN (SELECT event_id FROM event_players GROUP BY event_id HAVING COUNT(*)=8)`)
	specialID := f.scalar(t, `SELECT id FROM events WHERE name='特殊对战1'`)
	detail := f.request(t, 0, "GET", fmt.Sprintf("/api/public/events/%d", specialID), nil, 200)
	if !bytes.Contains(detail, []byte(`"ace_draw_source":"special"`)) || !bytes.Contains(detail, []byte(`"ace_draw_special":1`)) {
		t.Fatalf("历史大将战没有显示特殊抽选来源: %s", detail)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM (SELECT m.player_a_id FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.event_id=? AND r.stage='round1' GROUP BY m.player_a_id HAVING COUNT(*)>1)`, specialID)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='GSL 直播赛' AND status='round2'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='队长投票示例' AND status='captain_vote'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events e WHERE e.name='待分队示例' AND e.status='draft' AND NOT EXISTS (SELECT 1 FROM event_players p WHERE p.event_id=e.id)`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='第一轮排阵示例' AND status='lineup_confirm'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='KOF 阶段示例' AND status='round2'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='大将战示例' AND status='ace'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE name='结束确认示例' AND status='final_confirm'`)
	f.assertCount(t, 4, `SELECT COUNT(*) FROM matches WHERE round_no=3 AND status='finished'`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches m WHERE NOT EXISTS (SELECT 1 FROM event_players p WHERE p.event_id=m.event_id AND p.user_id=m.player_a_id AND p.team_id=m.team_a_id) OR NOT EXISTS (SELECT 1 FROM event_players p WHERE p.event_id=m.event_id AND p.user_id=m.player_b_id AND p.team_id=m.team_b_id)`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM rounds r WHERE score_a<>(SELECT COUNT(*) FROM matches m WHERE m.round_id=r.id AND m.status='finished' AND m.winner_team_id=m.team_a_id) OR score_b<>(SELECT COUNT(*) FROM matches m WHERE m.round_id=r.id AND m.status='finished' AND m.winner_team_id=m.team_b_id)`)
	f.assertCount(t, 0, `SELECT COUNT(*) FROM matches current JOIN matches previous ON previous.round_id=current.round_id AND previous.seq=current.seq-1 JOIN events e ON e.id=current.event_id WHERE e.name NOT LIKE '特殊对战%' AND current.round_no=2 AND ((previous.winner_team_id=previous.team_a_id AND previous.player_a_id<>current.player_a_id) OR (previous.winner_team_id=previous.team_b_id AND previous.player_b_id<>current.player_b_id))`)
	rows, err := f.app.db.Query(`SELECT username,password_hash FROM users WHERE username IN ('admin','2','3','4','5','6','7','8','9','10')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var username, hash string
		if err := rows.Scan(&username, &hash); err != nil {
			t.Fatal(err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte("test1234")); err != nil {
			t.Fatalf("测试账号 %s 的密码不是 test1234", username)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 10 {
		t.Fatalf("测试账号数为 %d，期望 10", seen)
	}
}

func TestWriteDemoDatabaseForExplicitOutput(t *testing.T) {
	output := os.Getenv("TEAM_BATTLE_TEST_DB_OUTPUT")
	if output == "" {
		t.Skip("未指定演示数据库输出路径")
	}
	path, err := filepath.Abs(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("拒绝覆盖已有数据库: %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := seedTestData(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("演示数据库已创建: %s", path)
}
