package app

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
)

// 抽签快照与具体对局绑定，比分纠错删除对局后，旧记录只留在审计中。
type aceDrawResult struct {
	MatchID      int64     `json:"match_id"`
	Source       string    `json:"source"`
	Special      int       `json:"special,omitempty"`
	Rules        []string  `json:"rules,omitempty"`
	CandidateIDs [][]int64 `json:"candidate_ids,omitempty"`
	PlayerAID    int64     `json:"player_a_id,omitempty"`
	PlayerBID    int64     `json:"player_b_id,omitempty"`
	Adjusted     bool      `json:"adjusted,omitempty"`
}

func (a *App) loadAceDrawResult(eventID int64) (*aceDrawResult, error) {
	var matchID, playerA, playerB int64
	var payload string
	err := a.db.QueryRow(`SELECT m.id,m.player_a_id,m.player_b_id,a.payload FROM matches m JOIN event_actions a ON a.id=(SELECT id FROM event_actions WHERE event_id=m.event_id AND action='ace_drawn' ORDER BY id DESC LIMIT 1) WHERE m.event_id=? AND m.round_no=3 ORDER BY m.id DESC LIMIT 1`, eventID).Scan(&matchID, &playerA, &playerB, &payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var draw aceDrawResult
	if err := json.Unmarshal([]byte(payload), &draw); err != nil {
		return nil, err
	}
	if draw.MatchID != 0 && draw.MatchID != matchID {
		return nil, nil
	}
	// 兼容旧历史的来源记录，不用当前规则补造当时没有保存的候选池。
	if draw.Source == "" && len(draw.Rules) > 0 {
		draw.Source = draw.Rules[0]
		for _, source := range draw.Rules[1:] {
			if source != draw.Source {
				draw.Source = "mixed"
				break
			}
		}
	}
	if draw.Source == "" {
		return nil, nil
	}
	if draw.Source == "random_fallback" {
		draw.Source = "random"
	}
	if strings.HasPrefix(draw.Source, "special:") {
		if index, err := strconv.Atoi(strings.TrimPrefix(draw.Source, "special:")); err == nil && index >= 0 {
			draw.Source, draw.Special = "special", index+1
		}
	}
	draw.MatchID = matchID
	draw.Adjusted = (draw.PlayerAID != 0 && draw.PlayerAID != playerA) || (draw.PlayerBID != 0 && draw.PlayerBID != playerB)
	return &draw, nil
}
