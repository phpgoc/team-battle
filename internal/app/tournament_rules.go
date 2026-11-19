package app

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
	TeamAIDs    []int64 `json:"team_a_ids,omitempty"`
	TeamBIDs    []int64 `json:"team_b_ids,omitempty"`
	Probability int     `json:"probability"`
}

// 新规则按两侧分别保存；旧规则仍将同一候选名单与实际队伍取交集。
func (s AceSpecial) candidateIDs(side int) []int64 {
	if s.TeamAIDs != nil || s.TeamBIDs != nil {
		if side == 0 {
			return s.TeamAIDs
		}
		return s.TeamBIDs
	}
	return s.PlayerIDs
}

func (s AceSpecial) allPlayerIDs() []int64 {
	if s.TeamAIDs == nil && s.TeamBIDs == nil {
		return s.PlayerIDs
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, side := range [][]int64{s.TeamAIDs, s.TeamBIDs} {
		for _, id := range side {
			if !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	return ids
}

type TournamentRules struct {
	Round1Mode         string       `json:"round1_mode"`
	KOFStarter         string       `json:"kof_starter"`
	KOFRevivalNoRepeat bool         `json:"kof_revival_no_repeat"`
	NoRepeat           bool         `json:"no_repeat"`
	RandomDrawSeconds  int          `json:"random_draw_seconds"`
	AceDrawSeconds     int          `json:"ace_draw_seconds"`
	AceMostWins        int          `json:"ace_most_wins"`
	AceMostLosses      int          `json:"ace_most_losses"`
	AceSpecials        []AceSpecial `json:"ace_specials"`
}

func defaultRules() TournamentRules {
	return TournamentRules{Round1Mode: "manual", KOFStarter: "random", NoRepeat: true, RandomDrawSeconds: 5, AceDrawSeconds: 5, AceMostWins: 20, AceMostLosses: 20, AceSpecials: []AceSpecial{}}
}

// 兼容旧赛事未保存首发方式的规则，缺省时使用随机首发。
func (r *TournamentRules) UnmarshalJSON(data []byte) error {
	type plainRules TournamentRules
	defaults := plainRules(defaultRules())
	if err := json.Unmarshal(data, &defaults); err != nil {
		return err
	}
	*r = TournamentRules(defaults)
	return nil
}

func decodeRules(raw string) (TournamentRules, error) {
	rules := defaultRules()
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return rules, err
	}
	return rules, rules.validateStored()
}

func (r TournamentRules) validate() error {
	if err := r.validateStored(); err != nil {
		return err
	}
	for _, special := range r.AceSpecials {
		if special.Probability == 0 {
			return errors.New("特殊对战概率必须大于0%")
		}
	}
	return nil
}

// 旧赛事允许保留零概率特殊组，读取时不能改变组号或历史抽签来源。
func (r TournamentRules) validateStored() error {
	if r.Round1Mode != "manual" && r.Round1Mode != "random" {
		return errors.New("第一轮模式只能为手动或随机")
	}
	if r.KOFStarter != "random" && r.KOFStarter != "captain" {
		return errors.New("KOF首发方式只能为随机首发或队长派人")
	}
	if r.RandomDrawSeconds < 0 || r.AceDrawSeconds < 0 {
		return errors.New("抽选时间不能为负数")
	}
	total := r.AceMostWins + r.AceMostLosses
	if r.AceMostWins < 0 || r.AceMostLosses < 0 {
		return errors.New("大将战概率不能为负数")
	}
	for _, special := range r.AceSpecials {
		if special.Probability < 0 || len(special.allPlayerIDs()) == 0 {
			return errors.New("特殊选择需要候选选手和有效概率")
		}
		for _, side := range [][]int64{special.candidateIDs(0), special.candidateIDs(1)} {
			seen := map[int64]bool{}
			for _, id := range side {
				if id <= 0 || seen[id] {
					return errors.New("特殊选择中的选手无效或重复")
				}
				seen[id] = true
			}
		}
		total += special.Probability
	}
	if total > 100 {
		return errors.New("大将战概率总和不能超过100%")
	}
	return nil
}

func normalizeStartTime(value string) (string, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02T15", "2006-01-02T15:04", "2006-01-02T15:04:05", "2006-01-02 15:04:05", time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02T15:00:00"), nil
		}
	}
	return "", errors.New("比赛开始时间格式不正确")
}

func saveRules(tx *sql.Tx, tournamentID int64, rules TournamentRules) error {
	raw, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE tournaments SET rules_json=? WHERE id=?`, string(raw), tournamentID)
	return err
}

// 将旧版本的分钟精度统一为整点时间。
func normalizeTournamentMetadata(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id,start_time FROM tournaments`)
	if err != nil {
		return err
	}
	type startTime struct {
		id    int64
		value string
	}
	times := []startTime{}
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			rows.Close()
			return err
		}
		normalized, err := normalizeStartTime(value)
		if err == nil && normalized != value {
			times = append(times, startTime{id, normalized})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range times {
		if _, err := tx.Exec(`UPDATE tournaments SET start_time=? WHERE id=?`, item.value, item.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
