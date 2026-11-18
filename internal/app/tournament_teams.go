package app

// 每场固定A、B两队，内部编号由赛事ID计算，不建立独立队伍表。
func fixedTeamIDs(eventID int64) []int64 {
	return []int64{eventID*2 - 1, eventID * 2}
}

func loadFixedTeamIDs(db sqlRunner, eventID int64) ([]int64, error) {
	var id int64
	if err := db.QueryRow(`SELECT id FROM tournaments WHERE id=?`, eventID).Scan(&id); err != nil {
		return nil, err
	}
	return fixedTeamIDs(id), nil
}

func lineupReadiness(db sqlRunner, eventID int64) ([2]bool, error) {
	var ready [2]bool
	err := db.QueryRow(`SELECT lineup_a_submitted OR EXISTS(SELECT 1 FROM lineup_entries WHERE event_id=t.id AND team_id=t.id*2-1),lineup_b_submitted OR EXISTS(SELECT 1 FROM lineup_entries WHERE event_id=t.id AND team_id=t.id*2) FROM tournaments t WHERE id=?`, eventID).Scan(&ready[0], &ready[1])
	return ready, err
}
