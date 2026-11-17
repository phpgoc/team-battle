package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand"
)

// 比赛推进在单个事务中完成，任何校验失败都不会留下半条比分。
type sqlRunner interface {
	Exec(string, ...any) (sql.Result, error)
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}
type matchEngine struct {
	db               sqlRunner
	eventID, actorID int64
	rng              *rand.Rand
}
type engineRound struct {
	id, winner   int64
	target, a, b int
	status       string
}
type engineMatch struct {
	id, roundID, teamA, teamB, playerA, playerB, winner, next int64
	seq                                                       int
	stage, status                                             string
}

func (e *matchEngine) round(stage string) (engineRound, error) {
	var r engineRound
	err := e.db.QueryRow(`SELECT id,target_score,score_a,score_b,COALESCE(winner_team_id,0),status FROM rounds WHERE event_id=? AND stage=?`, e.eventID, stage).Scan(&r.id, &r.target, &r.a, &r.b, &r.winner, &r.status)
	return r, err
}
func (e *matchEngine) teams() ([]int64, error) {
	rows, err := e.db.Query(`SELECT id FROM teams WHERE event_id=? ORDER BY id`, e.eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) != 2 {
		return nil, errors.New("赛事需要两支队伍")
	}
	return ids, rows.Err()
}
func (e *matchEngine) players(team int64) ([]int64, error) {
	rows, err := e.db.Query(`SELECT user_id FROM event_players WHERE event_id=? AND team_id=? AND active=1 ORDER BY user_id`, e.eventID, team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func containsID(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func (e *matchEngine) match(id int64) (engineMatch, error) {
	var m engineMatch
	err := e.db.QueryRow(`SELECT m.id,m.round_id,m.seq,m.team_a_id,m.team_b_id,m.player_a_id,m.player_b_id,COALESCE(m.winner_team_id,0),COALESCE(m.next_player_id,0),r.stage,m.status FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.id=? AND m.event_id=?`, id, e.eventID).Scan(&m.id, &m.roundID, &m.seq, &m.teamA, &m.teamB, &m.playerA, &m.playerB, &m.winner, &m.next, &m.stage, &m.status)
	return m, err
}
func (e *matchEngine) rules() (EventRules, error) {
	var raw string
	err := e.db.QueryRow(`SELECT rules_json FROM events WHERE id=?`, e.eventID).Scan(&raw)
	if err != nil {
		return EventRules{}, err
	}
	return decodeRules(raw)
}
func (e *matchEngine) status() (string, error) {
	var s string
	err := e.db.QueryRow(`SELECT status FROM events WHERE id=?`, e.eventID).Scan(&s)
	return s, err
}
func (e *matchEngine) setStage(stage string) error {
	_, err := e.db.Exec(`UPDATE events SET status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, stage, e.eventID)
	return err
}
func (e *matchEngine) log(action string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = e.db.Exec(`INSERT INTO event_actions(event_id,actor_id,action,payload) VALUES(?,?,?,?)`, e.eventID, e.actorID, action, string(raw))
	return err
}
func (e *matchEngine) insertMatch(stage string, a, b int64) error {
	teams, err := e.teams()
	if err != nil {
		return err
	}
	r, err := e.round(stage)
	if err != nil {
		return err
	}
	var seq int
	if err := e.db.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM matches WHERE round_id=?`, r.id).Scan(&seq); err != nil {
		return err
	}
	_, err = e.db.Exec(`INSERT INTO matches(event_id,round_id,seq,team_a_id,team_b_id,player_a_id,player_b_id,status,created_by) VALUES(?,?,?,?,?,?,?,'pending',?)`, e.eventID, r.id, seq, teams[0], teams[1], a, b, e.actorID)
	return err
}
func (e *matchEngine) start() error {
	s, err := e.status()
	if err != nil {
		return err
	}
	if s != statusDraft && s != statusCaptainVote && s != statusLineup {
		return errors.New("当前阶段不能开始赛事")
	}
	teams, err := e.teams()
	if err != nil {
		return err
	}
	total := 0
	for _, team := range teams {
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return errors.New("两队都必须有选手")
		}
		total += len(ids)
	}
	if total < 6 || total > 12 {
		return errors.New("开始赛事时需要6到12名选手")
	}
	if err := e.fillLineups(false); err != nil {
		return err
	}
	if _, err := e.db.Exec(`UPDATE rounds SET status='active' WHERE event_id=? AND stage='round1'`, e.eventID); err != nil {
		return err
	}
	if err := e.setStage(stageRound1); err != nil {
		return err
	}
	if err := e.nextRound1(); err != nil {
		return err
	}
	return e.log("round1_started", nil)
}
func (e *matchEngine) fillLineups(force bool) error {
	s, err := e.status()
	if err != nil {
		return err
	}
	if s != statusDraft && s != statusCaptainVote && s != statusLineup {
		return errors.New("当前阶段不能随机排阵")
	}
	rules, err := e.rules()
	if err != nil {
		return err
	}
	if force || rules.Round1Mode == "random" {
		if _, err := e.db.Exec(`DELETE FROM lineup_entries WHERE event_id=?`, e.eventID); err != nil {
			return err
		}
	}
	teams, err := e.teams()
	if err != nil {
		return err
	}
	var total int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM event_players WHERE event_id=?`, e.eventID).Scan(&total); err != nil {
		return err
	}
	for _, team := range teams {
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		limit := min(len(ids), total/2)
		e.rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		used := map[int64]bool{}
		for pos := 1; pos <= limit; pos++ {
			var id int64
			if err := e.db.QueryRow(`SELECT player_id FROM lineup_entries WHERE event_id=? AND team_id=? AND position=?`, e.eventID, team, pos).Scan(&id); err == nil {
				used[id] = true
			}
		}
		for pos := 1; pos <= limit; pos++ {
			var existing int
			if err := e.db.QueryRow(`SELECT COUNT(*) FROM lineup_entries WHERE event_id=? AND team_id=? AND position=?`, e.eventID, team, pos).Scan(&existing); err != nil {
				return err
			}
			if existing > 0 {
				continue
			}
			var chosen int64
			for _, id := range ids {
				if !used[id] {
					chosen = id
					break
				}
			}
			if chosen == 0 {
				return errors.New("排阵中选手重复")
			}
			used[chosen] = true
			if _, err := e.db.Exec(`INSERT INTO lineup_entries(event_id,team_id,position,player_id,source) VALUES(?,?,?,?,'random')`, e.eventID, team, pos, chosen); err != nil {
				return err
			}
		}
	}
	return nil
}
func (e *matchEngine) nextRound1() error {
	r, err := e.round(stageRound1)
	if err != nil {
		return err
	}
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE round_id=?`, r.id).Scan(&count); err != nil {
		return err
	}
	teams, err := e.teams()
	if err != nil {
		return err
	}
	rules, err := e.rules()
	if err != nil {
		return err
	}
	chosen := []int64{}
	for _, team := range teams {
		var id int64
		err := e.db.QueryRow(`SELECT player_id FROM lineup_entries WHERE event_id=? AND team_id=? AND position=?`, e.eventID, team, count+1).Scan(&id)
		if err == nil {
			chosen = append(chosen, id)
			continue
		}
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		candidates := []int64{}
		minCount := int(^uint(0) >> 1)
		var previous int64
		_ = e.db.QueryRow(`SELECT CASE WHEN team_a_id=? THEN player_a_id ELSE player_b_id END FROM matches WHERE round_id=? ORDER BY seq DESC LIMIT 1`, team, r.id).Scan(&previous)
		for _, player := range ids {
			var appearances int
			if err := e.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE round_id=? AND (player_a_id=? OR player_b_id=?)`, r.id, player, player).Scan(&appearances); err != nil {
				return err
			}
			if rules.NoRepeat && len(ids) > 1 && player == previous {
				continue
			}
			if rules.AllFirst && appearances < minCount {
				minCount = appearances
				candidates = nil
			}
			if !rules.AllFirst || appearances == minCount {
				candidates = append(candidates, player)
			}
		}
		if len(candidates) == 0 {
			return errors.New("没有可出场选手")
		}
		chosen = append(chosen, candidates[e.rng.Intn(len(candidates))])
	}
	return e.insertMatch(stageRound1, chosen[0], chosen[1])
}
func (e *matchEngine) record(m engineMatch, winner int64, note string) error {
	s, err := e.status()
	if err != nil {
		return err
	}
	if s != m.stage || m.status != statusPending {
		return errors.New("当前阶段不能录入该对局，或结果已经录入")
	}
	if winner != m.teamA && winner != m.teamB {
		return errors.New("胜者队伍无效")
	}
	if _, err := e.db.Exec(`UPDATE matches SET winner_team_id=?,status='finished',note=?,finished_at=CURRENT_TIMESTAMP WHERE id=?`, winner, note, m.id); err != nil {
		return err
	}
	m.winner = winner
	m.status = statusFinished
	if err := e.afterResult(m); err != nil {
		return err
	}
	return e.log("match_result_submitted", map[string]any{"match_id": m.id, "winner_team_id": winner})
}
func (e *matchEngine) afterResult(m engineMatch) error {
	var a, b int
	if err := e.db.QueryRow(`SELECT COALESCE(SUM(winner_team_id=team_a_id),0),COALESCE(SUM(winner_team_id=team_b_id),0) FROM matches WHERE round_id=? AND status='finished'`, m.roundID).Scan(&a, &b); err != nil {
		return err
	}
	r, err := e.round(m.stage)
	if err != nil {
		return err
	}
	winner := int64(0)
	status := statusActive
	if a >= r.target {
		winner = m.teamA
	}
	if b >= r.target {
		winner = m.teamB
	}
	if winner != 0 {
		status = statusFinished
	}
	if _, err := e.db.Exec(`UPDATE rounds SET score_a=?,score_b=?,winner_team_id=?,status=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, a, b, nullableInt64(winner), status, m.roundID); err != nil {
		return err
	}
	if winner != 0 {
		switch m.stage {
		case stageRound1:
			if err := e.setStage(stageRound2); err != nil {
				return err
			}
			if _, err := e.db.Exec(`UPDATE rounds SET status='active' WHERE event_id=? AND stage='round2'`, e.eventID); err != nil {
				return err
			}
			return e.initialKOF()
		case stageRound2:
			first, err := e.round(stageRound1)
			if err != nil {
				return err
			}
			if first.winner == winner {
				return e.setStage(statusFinalConfirm)
			}
			if err := e.setStage(stageAce); err != nil {
				return err
			}
			if _, err := e.db.Exec(`UPDATE rounds SET status='active' WHERE event_id=? AND stage='ace'`, e.eventID); err != nil {
				return err
			}
			return e.drawAce()
		case stageAce:
			return e.setStage(statusFinalConfirm)
		}
	}
	if m.stage == stageRound1 {
		return e.nextRound1()
	}
	if m.stage == stageRound2 {
		_, team := m.loser()
		random, err := e.kofRandom(m.roundID, team)
		if err != nil {
			return err
		}
		if random {
			ids, err := e.players(team)
			if err != nil {
				return err
			}
			return e.chooseKOF(m, ids[e.rng.Intn(len(ids))])
		}
	}
	return nil
}
func (m engineMatch) loser() (int64, int64) {
	if m.winner == m.teamA {
		return m.playerB, m.teamB
	}
	return m.playerA, m.teamA
}
func (e *matchEngine) lost(round, team, player int64) (bool, error) {
	var n int
	err := e.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE round_id=? AND status='finished' AND winner_team_id<>? AND ((team_a_id=? AND player_a_id=?) OR (team_b_id=? AND player_b_id=?))`, round, team, team, player, team, player).Scan(&n)
	return n > 0, err
}
func (e *matchEngine) kofRandom(round, team int64) (bool, error) {
	ids, err := e.players(team)
	if err != nil {
		return false, err
	}
	var target int
	if err := e.db.QueryRow(`SELECT target_score FROM rounds WHERE id=?`, round).Scan(&target); err != nil {
		return false, err
	}
	if target <= len(ids) || len(ids) == 0 {
		return false, nil
	}
	for _, id := range ids {
		lost, err := e.lost(round, team, id)
		if err != nil {
			return false, err
		}
		if !lost {
			return false, nil
		}
	}
	return true, nil
}
func (e *matchEngine) chooseKOF(m engineMatch, player int64) error {
	_, team := m.loser()
	ids, err := e.players(team)
	if err != nil {
		return err
	}
	if !containsID(ids, player) {
		return errors.New("换人选手不属于败方队伍")
	}
	if m.next != 0 {
		return errors.New("这场对局已经完成换人")
	}
	var later int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE round_id=? AND seq>?`, m.roundID, m.seq).Scan(&later); err != nil {
		return err
	}
	if later > 0 {
		return errors.New("下一场对局已经生成")
	}
	if _, err := e.db.Exec(`UPDATE matches SET next_player_id=? WHERE id=?`, player, m.id); err != nil {
		return err
	}
	a, b := m.playerA, m.playerB
	if team == m.teamA {
		a = player
	} else {
		b = player
	}
	return e.insertMatch(stageRound2, a, b)
}
func (e *matchEngine) initialKOF() error {
	// 重走首轮结果时不能沿用此前的队长首发选择。
	if _, err := e.db.Exec(`DELETE FROM player_selections WHERE event_id=? AND stage='round2'`, e.eventID); err != nil {
		return err
	}
	rules, err := e.rules()
	if err != nil {
		return err
	}
	if rules.KOFStarter == "captain" {
		return nil
	}
	teams, err := e.teams()
	if err != nil {
		return err
	}
	chosen := []int64{}
	for _, team := range teams {
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return errors.New("队伍没有可出场选手")
		}
		chosen = append(chosen, ids[e.rng.Intn(len(ids))])
	}
	return e.insertMatch(stageRound2, chosen[0], chosen[1])
}

func (e *matchEngine) selectKOFStarter(teamID, playerID int64, manager bool) error {
	status, err := e.status()
	if err != nil {
		return err
	}
	if status != stageRound2 {
		return errors.New("只有进入KOF后才能选择首发")
	}
	rules, err := e.rules()
	if err != nil {
		return err
	}
	if rules.KOFStarter != "captain" {
		return errors.New("该赛事使用随机首发")
	}
	teams, err := e.teams()
	if err != nil {
		return err
	}
	if !containsID(teams, teamID) {
		return errors.New("队伍不存在")
	}
	var matches int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM matches m JOIN rounds r ON r.id=m.round_id WHERE m.event_id=? AND r.stage='round2'`, e.eventID).Scan(&matches); err != nil {
		return err
	}
	if matches > 0 {
		return errors.New("KOF已经生成对局，不能重新选择首发")
	}
	ids, err := e.players(teamID)
	if err != nil {
		return err
	}
	if !containsID(ids, playerID) {
		return errors.New("首发必须属于对应队伍")
	}
	if !manager {
		var captain int
		if err := e.db.QueryRow(`SELECT is_captain FROM event_players WHERE event_id=? AND team_id=? AND user_id=? AND active=1`, e.eventID, teamID, e.actorID).Scan(&captain); err != nil || captain != 1 {
			return errors.New("只有本队队长或赛事管理员可以派出首发")
		}
		var selected int
		if err := e.db.QueryRow(`SELECT COUNT(*) FROM player_selections WHERE event_id=? AND stage='round2' AND team_id=?`, e.eventID, teamID).Scan(&selected); err != nil {
			return err
		}
		if selected > 0 {
			return errors.New("本队已提交首发，如需调整请联系赛事管理员")
		}
	}
	if _, err := e.db.Exec(`INSERT INTO player_selections(event_id,stage,team_id,player_id) VALUES(?,'round2',?,?) ON CONFLICT(event_id,stage,team_id) DO UPDATE SET player_id=excluded.player_id`, e.eventID, teamID, playerID); err != nil {
		return err
	}
	if err := e.log("kof_starter_selected", map[string]any{"team_id": teamID, "player_id": playerID}); err != nil {
		return err
	}
	chosen := []int64{}
	for _, team := range teams {
		var player int64
		err := e.db.QueryRow(`SELECT player_id FROM player_selections WHERE event_id=? AND stage='round2' AND team_id=?`, e.eventID, team).Scan(&player)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		chosen = append(chosen, player)
	}
	return e.insertMatch(stageRound2, chosen[0], chosen[1])
}

func (e *matchEngine) drawAce() error {
	teams, err := e.teams()
	if err != nil {
		return err
	}
	rules, err := e.rules()
	if err != nil {
		return err
	}
	chosen := []int64{}
	sources := []string{}
	for _, team := range teams {
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		roll := e.rng.Intn(100)
		candidates := []int64{}
		source := "random"
		if roll < rules.AceMostWins || roll < rules.AceMostWins+rules.AceMostLosses {
			wins := roll < rules.AceMostWins
			best := -1
			for _, id := range ids {
				var count int
				query := `SELECT COUNT(*) FROM matches WHERE event_id=? AND status='finished' AND winner_team_id= ? AND (player_a_id=? OR player_b_id=?)`
				if !wins {
					query = `SELECT COUNT(*) FROM matches WHERE event_id=? AND status='finished' AND winner_team_id<> ? AND (player_a_id=? OR player_b_id=?)`
				}
				if err := e.db.QueryRow(query, e.eventID, team, id, id).Scan(&count); err != nil {
					return err
				}
				if count > best {
					best = count
					candidates = nil
				}
				if count == best {
					candidates = append(candidates, id)
				}
			}
			source = "most_losses"
			if wins {
				source = "most_wins"
			}
		} else {
			cutoff := rules.AceMostWins + rules.AceMostLosses
			for _, group := range rules.AceSpecials {
				cutoff += group.Probability
				if roll < cutoff {
					for _, id := range group.PlayerIDs {
						if containsID(ids, id) {
							candidates = append(candidates, id)
						}
					}
					source = "special"
					break
				}
			}
		}
		if len(candidates) == 0 {
			candidates = ids
			source = "random"
		}
		chosen = append(chosen, candidates[e.rng.Intn(len(candidates))])
		sources = append(sources, source)
	}
	if err := e.insertMatch(stageAce, chosen[0], chosen[1]); err != nil {
		return err
	}
	return e.log("ace_drawn", map[string]any{"player_a_id": chosen[0], "player_b_id": chosen[1], "rules": sources})
}

// 修改早先结果会使之后的出场和阶段失效，回到修正位置重新推进。
func (e *matchEngine) correct(m engineMatch, winner int64, note string) error {
	if m.status != statusFinished || (winner != m.teamA && winner != m.teamB) {
		return errors.New("只能修正已录入对局的合法胜者")
	}
	stages := []string{stageRound1, stageRound2, stageAce}
	index := 0
	for i, s := range stages {
		if s == m.stage {
			index = i
		}
	}
	if _, err := e.db.Exec(`DELETE FROM matches WHERE round_id=? AND seq>?`, m.roundID, m.seq); err != nil {
		return err
	}
	for _, stage := range stages[index+1:] {
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
	if _, err := e.db.Exec(`UPDATE matches SET winner_team_id=?,next_player_id=NULL,note=? WHERE id=?`, winner, note, m.id); err != nil {
		return err
	}
	if err := e.setStage(m.stage); err != nil {
		return err
	}
	m.winner = winner
	m.next = 0
	if err := e.afterResult(m); err != nil {
		return err
	}
	return e.log("match_result_corrected", map[string]any{"match_id": m.id, "winner_team_id": winner, "later_matches_reset": true})
}
