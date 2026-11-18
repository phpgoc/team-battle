package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand"
	"strconv"
	"strings"
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
	var setupEntries int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM lineup_entries WHERE event_id=? AND source='setup_draw'`, e.eventID).Scan(&setupEntries); err != nil {
		return err
	}
	if !force && setupEntries > 0 {
		return nil
	}
	// 已公开的随机排阵同样是正式结果，仅显式重新抽签时才清空。
	if force {
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
			if appearances < minCount {
				minCount = appearances
				candidates = nil
			}
			if appearances == minCount {
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
	// 消耗败方候选；胜方候选在生成下一局时继续保留，直到本队需要换人。
	var reserved int64
	if m.stage == stageRound2 {
		_, loser := m.loser()
		err := e.db.QueryRow(`SELECT player_id FROM kof_reservations WHERE match_id=? AND team_id=?`, m.id, loser).Scan(&reserved)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if _, err := e.db.Exec(`DELETE FROM kof_reservations WHERE match_id=? AND team_id=?`, m.id, loser); err != nil {
			return err
		}
	}
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
			if _, err := e.db.Exec(`DELETE FROM kof_reservations WHERE event_id=?`, e.eventID); err != nil {
				return err
			}
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
			// KOF结束只进入待抽签阶段，管理员核对录分后再手动抽取大将。
			if _, err := e.db.Exec(`UPDATE rounds SET status='pending' WHERE event_id=? AND stage='ace'`, e.eventID); err != nil {
				return err
			}
			return nil
		case stageAce:
			return e.setStage(statusFinalConfirm)
		}
	}
	if m.stage == stageRound1 {
		return e.nextRound1()
	}
	if m.stage == stageRound2 {
		_, team := m.loser()
		if reserved != 0 {
			lost, err := e.lost(m.roundID, team, reserved)
			if err != nil {
				return err
			}
			ids, err := e.players(team)
			if err != nil {
				return err
			}
			if !lost && containsID(ids, reserved) {
				return e.chooseKOF(m, reserved)
			}
		}
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
	if err := e.insertMatch(stageRound2, a, b); err != nil {
		return err
	}
	// 候选跟随胜方进入下一局，不公开，也不因本队继续获胜而丢失。
	_, err = e.db.Exec(`UPDATE kof_reservations SET match_id=(SELECT id FROM matches WHERE round_id=? AND seq=?) WHERE match_id=? AND team_id=?`, m.roundID, m.seq+1, m.id, m.winner)
	return err
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
	var initialA, initialB sql.NullInt64
	if err := e.db.QueryRow(`SELECT kof_initial_a,kof_initial_b FROM events WHERE id=?`, e.eventID).Scan(&initialA, &initialB); err != nil {
		return err
	}
	if initialA.Valid && initialB.Valid {
		return e.insertMatch(stageRound2, initialA.Int64, initialB.Int64)
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
		if err := e.authorizeKOFSelection(teamID, playerID, false); err != nil {
			return err
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

// 手动抽签须在两轮各胜一轮后执行，重复点击不能重新选择大将。
func (e *matchEngine) beginAceDraw() error {
	stage, err := e.status()
	if err != nil {
		return err
	}
	if stage != stageAce {
		return errors.New("当前阶段不能抽取大将")
	}
	first, err := e.round(stageRound1)
	if err != nil {
		return err
	}
	second, err := e.round(stageRound2)
	if err != nil {
		return err
	}
	if first.status != statusFinished || second.status != statusFinished || first.winner == 0 || second.winner == 0 || first.winner == second.winner {
		return errors.New("前两轮必须各胜一轮才能抽取大将")
	}
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM matches WHERE event_id=? AND round_no=3`, e.eventID).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("大将战已抽签，不能重复抽取")
	}
	if _, err := e.db.Exec(`UPDATE rounds SET status='active' WHERE event_id=? AND stage='ace'`, e.eventID); err != nil {
		return err
	}
	return e.drawAce()
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
	// 规则抽签只进行一次，双方使用同一个规则来源；每一方再在自己的候选人中抽取。
	roll := e.rng.Intn(100)
	mode := "random"
	if roll < rules.AceMostWins {
		mode = "most_wins"
	} else if roll < rules.AceMostWins+rules.AceMostLosses {
		mode = "most_losses"
	} else {
		cutoff := rules.AceMostWins + rules.AceMostLosses
		for index, group := range rules.AceSpecials {
			cutoff += group.Probability
			if roll < cutoff {
				mode = "special:" + strconv.Itoa(index)
				break
			}
		}
	}

	candidatesByTeam := make([][]int64, 0, len(teams))
	sources := make([]string, 0, len(teams))
	for side, team := range teams {
		ids, err := e.players(team)
		if err != nil {
			return err
		}
		candidates := append([]int64(nil), ids...)
		source := mode
		switch {
		case mode == "most_wins":
			candidates, err = e.aceRecordCandidates(ids, true)
		case mode == "most_losses":
			candidates, err = e.aceRecordCandidates(ids, false)
		case strings.HasPrefix(mode, "special:"):
			index, _ := strconv.Atoi(strings.TrimPrefix(mode, "special:"))
			candidates = nil
			if index >= 0 && index < len(rules.AceSpecials) {
				for _, id := range rules.AceSpecials[index].candidateIDs(side) {
					if containsID(ids, id) {
						candidates = append(candidates, id)
					}
				}
			}
			if len(candidates) == 0 {
				candidates = append([]int64(nil), ids...)
				source = "random_fallback"
			}
		}
		if err != nil {
			return err
		}
		if len(candidates) == 0 {
			return errors.New("大将战没有可抽取的选手")
		}
		candidatesByTeam = append(candidatesByTeam, candidates)
		sources = append(sources, source)
	}
	chosen := make([]int64, 0, len(candidatesByTeam))
	for _, candidates := range candidatesByTeam {
		chosen = append(chosen, candidates[e.rng.Intn(len(candidates))])
	}
	if err := e.insertMatch(stageAce, chosen[0], chosen[1]); err != nil {
		return err
	}
	return e.log("ace_drawn", map[string]any{"player_a_id": chosen[0], "player_b_id": chosen[1], "source": mode, "rules": sources, "candidate_counts": []int{len(candidatesByTeam[0]), len(candidatesByTeam[1])}})
}

type aceRecord struct {
	playerID     int64
	wins, losses int
}

// 大将战只统计前两轮的已完成对局，不把大将战本身的结果带入抽选。
func (e *matchEngine) aceRecordCandidates(ids []int64, mostWins bool) ([]int64, error) {
	if len(ids) == 0 {
		return nil, errors.New("大将战没有可抽取的选手")
	}
	records := make([]aceRecord, 0, len(ids))
	for _, id := range ids {
		var wins, losses int
		if err := e.db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN ((player_a_id=? AND winner_team_id=team_a_id) OR (player_b_id=? AND winner_team_id=team_b_id)) THEN 1 ELSE 0 END),0), COALESCE(SUM(CASE WHEN ((player_a_id=? AND winner_team_id=team_b_id) OR (player_b_id=? AND winner_team_id=team_a_id)) THEN 1 ELSE 0 END),0) FROM matches WHERE event_id=? AND status='finished' AND round_id IN (SELECT id FROM rounds WHERE event_id=? AND stage IN ('round1','round2'))`, id, id, id, id, e.eventID, e.eventID).Scan(&wins, &losses); err != nil {
			return nil, err
		}
		records = append(records, aceRecord{playerID: id, wins: wins, losses: losses})
	}
	best := records[0]
	for _, record := range records[1:] {
		if betterAceRecord(record, best, mostWins) {
			best = record
		}
	}
	candidates := []int64{}
	for _, record := range records {
		if record.wins == best.wins && record.losses == best.losses {
			candidates = append(candidates, record.playerID)
		}
	}
	return candidates, nil
}

func betterAceRecord(candidate, current aceRecord, mostWins bool) bool {
	if mostWins {
		candidateDiff := candidate.wins - candidate.losses
		currentDiff := current.wins - current.losses
		return candidateDiff > currentDiff || (candidateDiff == currentDiff && candidate.wins > current.wins)
	}
	candidateDiff := candidate.losses - candidate.wins
	currentDiff := current.losses - current.wins
	// 多败在胜负差相同时优先胜场更少者，例如 2:2 优先于 3:3。
	return candidateDiff > currentDiff || (candidateDiff == currentDiff && candidate.wins < current.wins)
}

// 修改早先结果会使之后的出场和阶段失效，回到修正位置重新推进。
func (e *matchEngine) correct(m engineMatch, winner int64, note string) error {
	if m.status != statusFinished || (winner != m.teamA && winner != m.teamB) {
		return errors.New("只能修正已录入对局的合法胜者")
	}
	if winner == m.winner {
		return errors.New("胜负未变化，无需修正")
	}
	// 修正历史比分时，所有依赖原结果的预报名作废。
	if _, err := e.db.Exec(`DELETE FROM kof_reservations WHERE event_id=?`, e.eventID); err != nil {
		return err
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
