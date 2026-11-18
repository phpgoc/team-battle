package app

import (
	"bytes"
	"fmt"
	"testing"
)

func TestUnifiedMutableSettingsAndRollback(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "captain")
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/start", id), nil, 200)
	path := fmt.Sprintf("/api/events/%d/settings", id)
	f.request(t, 4, "PATCH", path, map[string]any{"random_draw_seconds": 2}, 403)
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": 2, "ace_draw_seconds": 7, "background_id": 0}, 200)
	before, _ := f.app.publicEventPayload(id)
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": 3, "background_id": 999}, 400)
	after, _ := f.app.publicEventPayload(id)
	if !bytes.Equal(before, after) {
		t.Fatal("无效背景未回滚")
	}
	f.request(t, 2, "PATCH", path, map[string]any{"random_draw_seconds": -1, "background_id": 0}, 400)
	if _, err := f.app.db.Exec(`INSERT INTO backgrounds(name,image_data,image_mime) VALUES('统一保存背景',X'','image/png')`); err != nil {
		t.Fatal(err)
	}
	background := f.scalar(t, `SELECT id FROM backgrounds WHERE name='统一保存背景'`)
	f.request(t, 2, "PATCH", path, map[string]any{"background_id": background, "random_draw_seconds": 3, "name": "非法改名"}, 400)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE id=? AND background_id IS NULL`, id)
	f.request(t, 2, "PATCH", path, map[string]any{"background_id": background, "random_draw_seconds": 2, "ace_draw_seconds": 7}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM events WHERE id=? AND background_id=?`, id, background)
	if rules := mustEventRules(f.app.db, id); rules.RandomDrawSeconds != 2 || rules.AceDrawSeconds != 7 {
		t.Fatal(rules)
	}
	if _, err := f.app.db.Exec(`UPDATE events SET status='completed' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	f.request(t, 1, "PATCH", path, map[string]any{"random_draw_seconds": 1}, 400)
}
