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
	KOFStarter    string       `json:"kof_starter"`
	NoRepeat      bool         `json:"no_repeat"`
	AllFirst      bool         `json:"all_first"`
	AceMostWins   int          `json:"ace_most_wins"`
	AceMostLosses int          `json:"ace_most_losses"`
	AceSpecials   []AceSpecial `json:"ace_specials"`
}

func defaultRules() EventRules {
	return EventRules{Round1Mode: "manual", KOFStarter: "random", NoRepeat: true, AllFirst: true, AceMostWins: 20, AceMostLosses: 20, AceSpecials: []AceSpecial{}}
}

// 兼容旧赛事未保存首发方式的规则，缺省时使用随机首发。
func (r *EventRules) UnmarshalJSON(data []byte) error {
	type plainRules EventRules
	defaults := plainRules(defaultRules())
	if err := json.Unmarshal(data, &defaults); err != nil {
		return err
	}
	*r = EventRules(defaults)
	return nil
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
	if r.KOFStarter != "random" && r.KOFStarter != "captain" {
		return errors.New("KOF首发方式只能为随机首发或队长派人")
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
	for _, layout := range []string{"2006-01-02T15", "2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02T15:00:00"), nil
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

// 将旧版本的队名和分钟精度统一为固定队名、整点时间。
func normalizeEventMetadata(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE teams SET name=CASE WHEN id=(SELECT MIN(t.id) FROM teams t WHERE t.event_id=teams.event_id) THEN 'A 队' ELSE 'B 队' END WHERE name<>CASE WHEN id=(SELECT MIN(t.id) FROM teams t WHERE t.event_id=teams.event_id) THEN 'A 队' ELSE 'B 队' END`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id,event_time FROM events`)
	if err != nil {
		return err
	}
	type eventTime struct {
		id    int64
		value string
	}
	times := []eventTime{}
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			rows.Close()
			return err
		}
		normalized, err := normalizeEventTime(value)
		if err == nil && normalized != value {
			times = append(times, eventTime{id, normalized})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range times {
		if _, err := tx.Exec(`UPDATE events SET event_time=? WHERE id=?`, item.value, item.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
