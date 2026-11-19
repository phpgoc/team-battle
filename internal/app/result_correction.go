package app

import (
	"database/sql"
	"errors"
	"slices"
)

type correctionState struct {
	match engineMatch
}

// 修正从选中的对局重新推进，校验、截断和录入在同一事务内完成。
func (e *matchEngine) correct(req tournamentResultCorrection) error {
	return e.correctOne(req)
}

func (e *matchEngine) correctBatch(corrections []tournamentResultCorrection) error {
	if len(corrections) < 2 {
		return errors.New("多局修正需要至少两场对局")
	}
	states, err := e.loadCorrectionStates()
	if err != nil {
		return err
	}
	indexByID := make(map[int64]int, len(states))
	for index := range states {
		indexByID[states[index].match.id] = index
	}
	lastFinishedID := int64(0)
	for _, state := range states {
		if state.match.status == statusFinished {
			lastFinishedID = state.match.id
		}
	}
	seen := make(map[int64]bool, len(corrections))
	for _, correction := range corrections {
		index, exists := indexByID[correction.MatchID]
		if !exists || seen[correction.MatchID] {
			return errors.New("修正的对局不存在或重复")
		}
		seen[correction.MatchID] = true
		current := &states[index].match
		if current.status != statusFinished {
			return errors.New("只能修正已录入的对局")
		}
		if correction.Expected == nil {
			return errors.New("请刷新后重新检查批量修正")
		}
		if err := e.checkCorrectionSnapshot(*current, *correction.Expected); err != nil {
			return err
		}
		if correction.WinnerTeamID != current.teamA && correction.WinnerTeamID != current.teamB {
			return errors.New("胜者队伍无效")
		}
		if correction.WinnerTeamID == current.winner && correction.PlayerAID == nil && correction.PlayerBID == nil {
			return errors.New("修正内容没有变化")
		}
		current.winner = correction.WinnerTeamID
		if correction.PlayerAID != nil {
			current.playerA = *correction.PlayerAID
		}
		if correction.PlayerBID != nil {
			current.playerB = *correction.PlayerBID
		}
		if current.id != lastFinishedID && (correction.PlayerAID != nil || correction.PlayerBID != nil) {
			return errors.New("只能修改最后一局已录分对局的出场选手")
		}
	}
	for _, correction := range corrections {
		if correction.PlayerAID == nil && correction.PlayerBID == nil {
			continue
		}
		match := states[indexByID[correction.MatchID]].match
		if match.stage == stageRound2 && match.seq == 1 {
			if _, err := e.db.Exec(`UPDATE tournaments SET kof_initial_a=?,kof_initial_b=? WHERE id=?`, match.playerA, match.playerB, e.tournamentID); err != nil {
				return err
			}
			if _, err := e.db.Exec(`UPDATE player_selections SET player_id=CASE team_id WHEN ? THEN ? ELSE ? END WHERE tournament_id=? AND stage='round2'`, match.teamA, match.playerA, match.playerB, e.tournamentID); err != nil {
				return err
			}
		}
	}
	if err := e.validateCorrectionTimeline(states); err != nil {
		return err
	}
	if len(groupsForStage(states, stageAce)) > 0 {
		for _, correction := range corrections {
			if states[indexByID[correction.MatchID]].match.stage != stageAce {
				return errors.New("大将战已经生成，需先单独修正大将战对局")
			}
		}
	}
	for _, correction := range corrections {
		index := indexByID[correction.MatchID]
		match := states[index].match
		if _, err := e.db.Exec(`UPDATE matches SET player_a_id=?,player_b_id=?,winner_team_id=?,next_player_id=NULL,note=COALESCE(?,note) WHERE id=?`, match.playerA, match.playerB, match.winner, correction.Note, match.id); err != nil {
			return err
		}
	}
	if _, err := e.db.Exec(`DELETE FROM kof_reservations WHERE tournament_id=?`, e.tournamentID); err != nil {
		return err
	}
	if err := e.recomputeCorrectionTimeline(states); err != nil {
		return err
	}
	return e.log("match_results_corrected", map[string]any{"corrections": corrections, "matches_preserved": true})
}

func (e *matchEngine) correctOne(req tournamentResultCorrection) error {
	before, err := e.match(req.MatchID)
	if err != nil {
		return errors.New("对局不存在")
	}
	if before.status != statusFinished {
		return errors.New("只能修正已录入的对局")
	}
	if req.Expected != nil {
		if err := e.checkCorrectionSnapshot(before, *req.Expected); err != nil {
			return err
		}
	}
	m := before
	if req.WinnerTeamID != 0 {
		m.winner = req.WinnerTeamID
	}
	if m.winner != m.teamA && m.winner != m.teamB {
		return errors.New("胜者队伍无效")
	}
	if req.PlayerAID != nil {
		m.playerA = *req.PlayerAID
	}
	if req.PlayerBID != nil {
		m.playerB = *req.PlayerBID
	}
	playersChanged := m.playerA != before.playerA || m.playerB != before.playerB
	if !playersChanged && m.winner == before.winner {
		return errors.New("对阵和胜负没有变化")
	}
	if playersChanged {
		if err := e.correctLastMatchPlayers(m); err != nil {
			return err
		}
	}
	rounds := []string{stageRound1, stageRound2, stageAce}
	index := slices.Index(rounds, m.stage)
	if index < 0 {
		return errors.New("对局轮次无效")
	}
	if _, err := e.db.Exec(`DELETE FROM kof_reservations WHERE tournament_id=?`, e.tournamentID); err != nil {
		return err
	}
	if _, err := e.db.Exec(`DELETE FROM matches WHERE round_id=? AND seq>?`, m.roundID, m.seq); err != nil {
		return err
	}
	for _, stage := range rounds[index+1:] {
		r, err := e.round(stage)
		if err != nil {
			return err
		}
		if _, err := e.db.Exec(`DELETE FROM matches WHERE round_id=?`, r.id); err != nil {
			return err
		}
		if _, err := e.db.Exec(`UPDATE rounds SET score_a=0,score_b=0,winner_team_id=NULL,status='pending' WHERE id=?`, r.id); err != nil {
			return err
		}
	}
	if _, err := e.db.Exec(`UPDATE matches SET player_a_id=?,player_b_id=?,winner_team_id=?,next_player_id=NULL,note=COALESCE(?,note) WHERE id=?`, m.playerA, m.playerB, m.winner, req.Note, m.id); err != nil {
		return err
	}
	if err := e.setStage(m.stage); err != nil {
		return err
	}
	m.next = 0
	if err := e.afterResult(m); err != nil {
		return err
	}
	return e.log("match_result_corrected", map[string]any{
		"match_id": m.id, "winner_team_id": m.winner, "player_a_id": m.playerA, "player_b_id": m.playerB,
		"previous_winner_team_id": before.winner, "previous_player_a_id": before.playerA, "previous_player_b_id": before.playerB,
		"later_matches_reset": true,
	})
}

func (e *matchEngine) loadCorrectionStates() ([]correctionState, error) {
	rows, err := e.db.Query(`SELECT m.id,m.round_id,m.seq,m.team_a_id,m.team_b_id,m.player_a_id,m.player_b_id,COALESCE(m.winner_team_id,0),COALESCE(m.next_player_id,0),r.stage,m.status
		FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.tournament_id=?
		ORDER BY CASE r.stage WHEN 'round1' THEN 1 WHEN 'round2' THEN 2 ELSE 3 END,m.seq`, e.tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []correctionState
	for rows.Next() {
		var match engineMatch
		if err := rows.Scan(&match.id, &match.roundID, &match.seq, &match.teamA, &match.teamB, &match.playerA, &match.playerB, &match.winner, &match.next, &match.stage, &match.status); err != nil {
			return nil, err
		}
		states = append(states, correctionState{match: match})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(states) == 0 {
		return nil, errors.New("没有可修正的对局")
	}
	return states, nil
}

func (e *matchEngine) validateCorrectionTimeline(states []correctionState) error {
	teams, err := e.teams()
	if err != nil {
		return err
	}
	rosters := [2][]int64{}
	for side := range teams {
		rosters[side], err = e.players(teams[side])
		if err != nil {
			return err
		}
	}
	targets := map[string]int{}
	for _, stage := range []string{stageRound1, stageRound2, stageAce} {
		round, err := e.round(stage)
		if err != nil {
			return err
		}
		targets[stage] = round.target
	}
	groups := map[string][]engineMatch{stageRound1: {}, stageRound2: {}, stageAce: {}}
	for _, state := range states {
		match := state.match
		if _, ok := groups[match.stage]; !ok {
			return errors.New("对局阶段无效")
		}
		if match.status != statusFinished && match.status != statusPending && match.status != "planned" {
			return errors.New("对局状态无效")
		}
		if !containsID(teams, match.teamA) || !containsID(teams, match.teamB) || match.teamA == match.teamB ||
			!containsID(rosters[0], match.playerA) || !containsID(rosters[1], match.playerB) {
			return errors.New("对局选手必须属于对应队伍")
		}
		if match.status == statusFinished && match.winner != match.teamA && match.winner != match.teamB {
			return errors.New("已录入对局的胜者无效")
		}
		groups[match.stage] = append(groups[match.stage], match)
	}
	score := func(matches []engineMatch, target int) ([2]int, int64, error) {
		result := [2]int{}
		winner := int64(0)
		for index, match := range matches {
			if match.seq != index+1 {
				return result, 0, errors.New("同一轮对局编号必须连续")
			}
			if winner != 0 {
				return result, 0, errors.New("达到目标胜场后不能继续录入对局")
			}
			if match.status != statusFinished {
				if index != len(matches)-1 {
					return result, 0, errors.New("未完成的对局之后不能有其他对局")
				}
				if match.winner != 0 {
					return result, 0, errors.New("未完成对局不能有胜者")
				}
			}
			if match.status == statusFinished {
				if match.winner == match.teamA {
					result[0]++
				} else {
					result[1]++
				}
				if result[0] > target || result[1] > target {
					return result, 0, errors.New("修正后比分超过该轮目标胜场")
				}
				if result[0] == target {
					winner = match.teamA
				} else if result[1] == target {
					winner = match.teamB
				}
			}
		}
		return result, winner, nil
	}
	_, firstWinner, err := score(groups[stageRound1], targets[stageRound1])
	if err != nil {
		return err
	}
	if len(groups[stageRound2]) > 0 && firstWinner == 0 {
		return errors.New("第一轮尚未达到目标胜场，不能保留KOF对局")
	}
	_, secondWinner, err := score(groups[stageRound2], targets[stageRound2])
	if err != nil {
		return err
	}
	if len(groups[stageAce]) > 0 {
		if firstWinner == 0 || secondWinner == 0 || firstWinner == secondWinner || len(groups[stageAce]) != 1 {
			return errors.New("当前比分不满足大将战条件")
		}
		ace := groups[stageAce][0]
		if ace.seq != 1 || ace.status == "planned" {
			return errors.New("大将战对局状态无效")
		}
	}
	if firstWinner != 0 && len(groups[stageRound2]) > 0 {
		kof := make([]archivedMatch, 0, len(groups[stageRound2]))
		for _, match := range groups[stageRound2] {
			if match.status != statusFinished {
				if len(kof) != len(groups[stageRound2])-1 {
					return errors.New("未完成KOF对局之后不能有其他对局")
				}
				kof = append(kof, archivedMatch{Round: 2, AID: match.playerA, BID: match.playerB, WhoWin: "a"})
			} else {
				whoWin := "a"
				if match.winner == match.teamB {
					whoWin = "b"
				}
				kof = append(kof, archivedMatch{Round: 2, AID: match.playerA, BID: match.playerB, WhoWin: whoWin})
			}
		}
		rules, err := e.rules()
		if err != nil {
			return err
		}
		if err := validateKOFSequence(kof, rosters, targets[stageRound2], rules.KOFRevivalNoRepeat); err != nil {
			return err
		}
	}
	return nil
}

func (e *matchEngine) recomputeCorrectionTimeline(states []correctionState) error {
	stages := []string{stageRound1, stageRound2, stageAce}
	winners := map[string]int64{}
	teams, err := e.teams()
	if err != nil {
		return err
	}
	for _, stage := range stages {
		round, err := e.round(stage)
		if err != nil {
			return err
		}
		var a, b int
		if err := e.db.QueryRow(`SELECT COALESCE(SUM(winner_team_id=team_a_id),0),COALESCE(SUM(winner_team_id=team_b_id),0) FROM matches WHERE round_id=? AND status='finished'`, round.id).Scan(&a, &b); err != nil {
			return err
		}
		winner, status := int64(0), statusPending
		if a >= round.target {
			winner, status = teams[0], statusFinished
		} else if b >= round.target {
			winner, status = teams[1], statusFinished
		} else if stage == stageAce {
			if winners[stageRound1] != 0 && winners[stageRound2] != 0 && winners[stageRound1] != winners[stageRound2] && len(groupsForStage(states, stageAce)) > 0 {
				status = statusActive
			}
		} else if len(groupsForStage(states, stage)) > 0 || (stage == stageRound2 && winners[stageRound1] != 0) {
			status = statusActive
		}
		if winner != 0 {
			winners[stage] = winner
		}
		if _, err := e.db.Exec(`UPDATE rounds SET score_a=?,score_b=?,winner_team_id=?,status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, a, b, nullableInt64(winner), status, round.id); err != nil {
			return err
		}
	}
	first, err := e.round(stageRound1)
	if err != nil {
		return err
	}
	second, err := e.round(stageRound2)
	if err != nil {
		return err
	}
	nextStage := stageRound1
	if first.winner != 0 {
		nextStage = stageRound2
	}
	if second.winner != 0 {
		if first.winner == second.winner {
			nextStage = statusFinalConfirm
		} else {
			nextStage = stageAce
		}
	}
	ace, err := e.round(stageAce)
	if err != nil {
		return err
	}
	if ace.winner != 0 {
		nextStage = statusFinalConfirm
	}
	if err := e.setStage(nextStage); err != nil {
		return err
	}
	for _, state := range states {
		if state.match.stage != stageRound2 || state.match.status != statusFinished {
			continue
		}
		var next engineMatch
		err := e.db.QueryRow(`SELECT id,seq,player_a_id,player_b_id,winner_team_id,team_a_id,team_b_id FROM matches WHERE round_id=? AND seq=?`, state.match.roundID, state.match.seq+1).Scan(&next.id, &next.seq, &next.playerA, &next.playerB, &next.winner, &next.teamA, &next.teamB)
		if err == nil {
			player := next.playerA
			if state.match.winner == state.match.teamA {
				player = next.playerB
			}
			if _, err := e.db.Exec(`UPDATE matches SET next_player_id=? WHERE id=?`, player, state.match.id); err != nil {
				return err
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	return nil
}

func groupsForStage(states []correctionState, stage string) []correctionState {
	var matches []correctionState
	for _, state := range states {
		if state.match.stage == stage {
			matches = append(matches, state)
		}
	}
	return matches
}

func (e *matchEngine) checkCorrectionSnapshot(m engineMatch, expected correctionSnapshot) error {
	var latestID, latestWinner int64
	if err := e.db.QueryRow(`SELECT id,COALESCE(winner_team_id,0) FROM matches WHERE tournament_id=? ORDER BY round_no DESC,seq DESC LIMIT 1`, e.tournamentID).Scan(&latestID, &latestWinner); err != nil {
		return err
	}
	if expected.MatchID != m.id || expected.PlayerAID != m.playerA || expected.PlayerBID != m.playerB || expected.WinnerTeamID != m.winner ||
		expected.LatestMatchID != latestID || expected.LatestWinnerTeamID != latestWinner {
		return errors.New("对局已变化，请重新检查后保存")
	}
	return nil
}

func (e *matchEngine) correctLastMatchPlayers(m engineMatch) error {
	var latestID int64
	if err := e.db.QueryRow(`SELECT id FROM matches WHERE tournament_id=? AND status='finished' ORDER BY round_no DESC,seq DESC LIMIT 1`, e.tournamentID).Scan(&latestID); err != nil {
		return err
	}
	if latestID != m.id {
		return errors.New("只能修改最后一局已录分对局的出场选手")
	}
	var rosters [2][]int64
	for side, team := range [2]int64{m.teamA, m.teamB} {
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		rosters[side] = ids
	}
	if !containsID(rosters[0], m.playerA) || !containsID(rosters[1], m.playerB) {
		return errors.New("对局选手必须属于对应队伍")
	}
	if m.stage != stageRound2 {
		return nil
	}
	if m.seq == 1 {
		if _, err := e.db.Exec(`UPDATE tournaments SET kof_initial_a=?,kof_initial_b=? WHERE id=?`, m.playerA, m.playerB, e.tournamentID); err != nil {
			return err
		}
	}
	prefix, err := e.kofCorrectionPrefix(m)
	if err != nil {
		return err
	}
	round, err := e.round(stageRound2)
	if err != nil {
		return err
	}
	rules, err := e.rules()
	if err != nil {
		return err
	}
	winner := "a"
	if m.winner == m.teamB {
		winner = "b"
	}
	// 共用历史录入的续战校验，保证胜者留场及复活顺序一致。
	if err := validateKOFSequence(append(prefix, archivedMatch{2, m.playerA, m.playerB, winner}), rosters, round.target, rules.KOFRevivalNoRepeat); err != nil {
		return err
	}
	if len(prefix) == 0 {
		// 实际出场与首发选择一起更新，之后从第一轮回退时仍使用原来的随机首发。
		_, err := e.db.Exec(`UPDATE player_selections SET player_id=CASE team_id WHEN ? THEN ? ELSE ? END WHERE tournament_id=? AND stage='round2'`, m.teamA, m.playerA, m.playerB, e.tournamentID)
		return err
	}
	next := m.playerB
	if prefix[len(prefix)-1].WhoWin == "b" {
		next = m.playerA
	}
	_, err = e.db.Exec(`UPDATE matches SET next_player_id=? WHERE round_id=? AND seq=?`, next, m.roundID, m.seq-1)
	return err
}

func (e *matchEngine) kofCorrectionPrefix(m engineMatch) ([]archivedMatch, error) {
	rows, err := e.db.Query(`SELECT player_a_id,player_b_id,who_win FROM matches WHERE round_id=? AND status='finished' AND seq<? ORDER BY seq`, m.roundID, m.seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var matches []archivedMatch
	for rows.Next() {
		match := archivedMatch{Round: 2}
		if err := rows.Scan(&match.AID, &match.BID, &match.WhoWin); err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, rows.Err()
}
