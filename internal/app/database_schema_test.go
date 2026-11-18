package app

import "testing"

func TestTournamentTableNamePreservesExistingEvents(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟旧数据库名称，启动初始化应保留ID、参赛者、对局和所有外键。
	if _, err := f.app.db.Exec(`ALTER TABLE tournaments RENAME TO events`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='events'`)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='tournaments'`)
	after, err := f.app.publicEventPayload(id)
	if err != nil || string(before) != string(after) {
		t.Fatalf("主表改名改变了赛事: %s %v", after, err)
	}
	rows, err := f.app.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("主表改名后存在无效外键")
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
}
