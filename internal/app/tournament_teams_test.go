package app

import (
	"fmt"
	"testing"
)

func TestFixedTeamInitializationPreservesResultsAndSequences(t *testing.T) {
	f := newWorkflowFixture(t)
	id := f.create(t, "random")
	f.enterKOF(t, id)
	before, err := f.app.publicEventPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	// 模拟旧队伍编号与赛事ID没有算术关系，确保转换实际关联而非猜测编号。
	if _, err := f.app.db.Exec(`CREATE TABLE teams(id INTEGER PRIMARY KEY,tournament_id INTEGER,name TEXT,color TEXT,lineup_submitted INTEGER DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.db.Exec(`INSERT INTO teams(id,tournament_id,name,color,lineup_submitted) VALUES(101,?,'旧A','',1),(205,?,'旧B','',0)`, id, id); err != nil {
		t.Fatal(err)
	}
	for table, columns := range map[string][]string{
		"tournament_players": {"team_id"}, "captain_votes": {"team_id"}, "lineup_entries": {"team_id"}, "rounds": {"winner_team_id"}, "matches": {"team_a_id", "team_b_id", "winner_team_id"}, "player_selections": {"team_id"}, "kof_reservations": {"team_id"},
	} {
		for _, column := range columns {
			if _, err := f.app.db.Exec(`UPDATE `+table+` SET `+column+`=CASE `+column+` WHEN ? THEN 101 WHEN ? THEN 205 ELSE `+column+` END`, id*2-1, id*2); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 空表也必须保留自增上限，防止历史审计中的旧ID被重新使用。
	if _, err := f.app.db.Exec(`INSERT INTO sqlite_sequence(name,seq) VALUES('captain_votes',1000)`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 0, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`)
	after, err := f.app.publicEventPayload(id)
	if err != nil || string(before) != string(after) {
		t.Fatalf("移除队伍表改变了赛事: %s %v", after, err)
	}
	if _, err := f.app.db.Exec(`INSERT INTO captain_votes(tournament_id,team_id,voter_id,target_id) VALUES(?,?,3,4)`, id, id*2-1); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM captain_votes WHERE id=1001`)
	if err := migrate(f.app.db); err != nil {
		t.Fatal(err)
	}
	f.assertCount(t, 1, `SELECT COUNT(*) FROM captain_votes WHERE id=1001`)
	// 转换之后仍须能继续合法录分，触发器也应继续维护who_win。
	match := f.scalar(t, `SELECT id FROM matches WHERE tournament_id=? AND status='pending'`, id)
	f.request(t, 2, "POST", fmt.Sprintf("/api/events/%d/matches/%d/result", id, match), map[string]any{"winner_team_id": id*2 - 1}, 200)
	f.assertCount(t, 1, `SELECT COUNT(*) FROM matches WHERE id=? AND who_win='a'`, match)
}
