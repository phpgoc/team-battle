package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// 大将战按互斥概率抽取规则；余下的概率始终交给全随机兜底。
type AceSpecial struct {
	PlayerIDs   []int64 `json:"player_ids"`
	Probability int     `json:"probability"`
}

type EventRules struct {
	Round1Mode    string       `json:"round1_mode"`
	NoRepeat      bool         `json:"no_repeat"`
	AllFirst      bool         `json:"all_first"`
	AceMostWins   int          `json:"ace_most_wins"`
	AceMostLosses int          `json:"ace_most_losses"`
	AceSpecials   []AceSpecial `json:"ace_specials"`
}

func defaultRules() EventRules {
	return EventRules{Round1Mode: "manual", NoRepeat: true, AllFirst: true, AceMostWins: 20, AceMostLosses: 20, AceSpecials: []AceSpecial{}}
}

func decodeRules(raw string) (EventRules, error) {
	rules := defaultRules()
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return rules, err
	}
	return rules, rules.validate()
}

func (r EventRules) validate() error {
	if r.Round1Mode != "manual" && r.Round1Mode != "random" {
		return errors.New("第一轮模式只能为手动或随机")
	}
	total := r.AceMostWins + r.AceMostLosses
	if r.AceMostWins < 0 || r.AceMostLosses < 0 {
		return errors.New("大将战概率不能为负数")
	}
	for _, special := range r.AceSpecials {
		if special.Probability < 0 || len(special.PlayerIDs) == 0 {
			return errors.New("特殊选择需要候选选手和有效概率")
		}
		seen := map[int64]bool{}
		for _, id := range special.PlayerIDs {
			if id <= 0 || seen[id] {
				return errors.New("特殊选择中的选手无效或重复")
			}
			seen[id] = true
		}
		total += special.Probability
	}
	if total > 100 {
		return errors.New("大将战概率总和不能超过100%")
	}
	return nil
}

func normalizeEventTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02T15:04:05"), nil
		}
	}
	return "", errors.New("比赛开始时间格式不正确")
}

func saveRules(tx *sql.Tx, eventID int64, rules EventRules) error {
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE events SET rules_json=? WHERE id=?`, string(raw), eventID)
	return err
}
