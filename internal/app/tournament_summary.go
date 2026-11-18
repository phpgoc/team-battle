package app

import "github.com/gin-gonic/gin"

// 首页使用阶段胜场呈现总比分，避免将三个阶段的小分相加造成误解。
func (a *App) publicTournamentSummaries(history bool) ([]map[string]any, error) {
	where, order := "e.status NOT IN ('completed','cancelled')", "e.updated_at DESC,e.id DESC"
	if history {
		where, order = "e.status='completed'", "e.start_time DESC,e.id DESC"
	}
	rows, err := a.db.Query(`SELECT e.id,e.name,e.description,e.start_time,e.status,e.round1_target,e.round2_target,e.created_at,COALESCE(e.manager_id,0),
		(SELECT COUNT(*) FROM tournament_players p WHERE p.tournament_id=e.id AND p.active=1),
		(SELECT COUNT(DISTINCT p.team_id) FROM tournament_players p WHERE p.tournament_id=e.id AND p.active=1),
		(SELECT COUNT(*) FROM rounds r WHERE r.tournament_id=e.id AND r.winner_team_id=(e.id*2-1)),
		(SELECT COUNT(*) FROM rounds r WHERE r.tournament_id=e.id AND r.winner_team_id=(e.id*2))
		FROM tournaments e WHERE ` + where + ` ORDER BY ` + order)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, round1, round2, managerID, players, assignedTeams, winsA, winsB int64
		var name, description, startTime, status, created string
		if err := rows.Scan(&id, &name, &description, &startTime, &status, &round1, &round2, &created, &managerID, &players, &assignedTeams, &winsA, &winsB); err != nil {
			return nil, err
		}
		items = append(items, gin.H{"id": id, "name": name, "description": description, "start_time": startTime, "status": status, "round1_target": round1, "round2_target": round2, "created_at": created, "manager_id": managerID, "has_roster": assignedTeams == 2, "player_count": players, "round_wins_a": winsA, "round_wins_b": winsB})
	}
	return items, rows.Err()
}
