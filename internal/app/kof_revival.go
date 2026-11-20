package app

import "slices"

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

// 全队已有败绩才开放复活报名。预报名按当前选手再输一局计算复活顺序。
func (e *matchEngine) kofNextCandidates(m engineMatch, team int64, ids []int64) (candidates []int64, randomRevival bool, err error) {
	if m.status == statusFinished && team == m.winner {
		return nil, false, nil
	}
	rows, err := e.db.Query(`SELECT CASE WHEN team_a_id=? THEN player_a_id ELSE player_b_id END,COUNT(*) FROM matches WHERE round_id=? AND seq<=? AND status='finished' AND winner_team_id<>? GROUP BY 1`, team, m.roundID, m.seq, team)
	if err != nil {
		return nil, false, err
	}
	losses := map[int64]int{}
	for rows.Next() {
		var id int64
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			rows.Close()
			return nil, false, err
		}
		losses[id] = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	current := m.playerA
	if team == m.teamB {
		current = m.playerB
	}
	hasFresh := false
	for _, id := range ids {
		if losses[id] == 0 {
			hasFresh = true
			if m.status != statusPending || id != current {
				candidates = append(candidates, id)
			}
		}
	}
	if hasFresh || len(ids) == 0 {
		return candidates, false, nil
	}
	round, err := e.round(stageRound2)
	if err != nil || round.target <= len(ids) || round.status == statusFinished {
		return nil, false, err
	}
	rules, err := e.rules()
	if err != nil {
		return nil, false, err
	}
	if m.status == statusPending {
		opponentScore := round.b
		if team == m.teamB {
			opponentScore = round.a
		}
		if opponentScore+1 >= round.target {
			return nil, false, nil
		}
		losses[current]++
	}
	candidates = slices.Clone(ids)
	if rules.KOFRevivalNoRepeat {
		candidates = leastKOFEliminations(ids, losses)
	}
	// 有其他合规人选时避免刚输的选手连续出场；预报名依然不能安排当前选手。
	if m.status == statusPending || (rules.NoRepeat && len(candidates) > 1) {
		candidates = slices.DeleteFunc(candidates, func(id int64) bool { return id == current })
	}
	return candidates, !rules.KOFManualRevival, nil
}
