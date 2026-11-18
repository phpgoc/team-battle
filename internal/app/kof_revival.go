package app

// 败方换人时全队均不在场，每人的淘汰次数等于已完成的出场轮数。
// 取最少淘汰者即可保证每轮复活不重复，整队轮完后自动进入下一轮。
func leastKOFEliminations(ids []int64, losses map[int64]int) []int64 {
	candidates := []int64{}
	minimum := int(^uint(0) >> 1)
	for _, id := range ids {
		if losses[id] < minimum {
			minimum = losses[id]
			candidates = nil
		}
		if losses[id] == minimum {
			candidates = append(candidates, id)
		}
	}
	return candidates
}

func (e *matchEngine) kofRevivalCandidates(round, team int64) ([]int64, error) {
	ids, err := e.players(team)
	if err != nil {
		return nil, err
	}
	rules, err := e.rules()
	if err != nil || !rules.KOFRevivalNoRepeat {
		return ids, err
	}
	rows, err := e.db.Query(`SELECT CASE WHEN team_a_id=? THEN player_a_id ELSE player_b_id END,COUNT(*) FROM matches WHERE round_id=? AND status='finished' AND winner_team_id<>? GROUP BY 1`, team, round, team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	losses := map[int64]int{}
	for rows.Next() {
		var id int64
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		losses[id] = count
	}
	return leastKOFEliminations(ids, losses), rows.Err()
}
