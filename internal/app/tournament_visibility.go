package app

type tournamentReadiness struct {
	playerCount int
	publicReady bool
}

func rosterReadiness(players, teams, setup int) tournamentReadiness {
	if players == 0 {
		return tournamentReadiness{playerCount: setup, publicReady: setup >= 6}
	}
	return tournamentReadiness{playerCount: players, publicReady: players >= 6 && teams == 2}
}

// 人数足够即可在分队前公开；人数不足的草稿仅供管理者查看。
func loadTournamentReadiness(db sqlRunner, id int64) (tournamentReadiness, error) {
	var players, teams, setup int
	err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM tournament_players p WHERE p.tournament_id=t.id AND p.active=1),(SELECT COUNT(DISTINCT team_id) FROM tournament_players p WHERE p.tournament_id=t.id AND p.active=1),(SELECT COUNT(*) FROM tournament_setup_roster s WHERE s.tournament_id=t.id) FROM tournaments t WHERE t.id=?`, id).Scan(&players, &teams, &setup)
	return rosterReadiness(players, teams, setup), err
}
