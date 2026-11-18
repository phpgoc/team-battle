package app

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// 赛前名单先保存选择意向，只有点击开始抽签才落入正式分队。
func saveSetupRoster(tx *sql.Tx, eventID int64, ids []int64, fixed map[string]string) error {
	if len(ids) > 12 {
		return errors.New("参赛列表最多12人")
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			return errors.New("参赛选手不能重复")
		}
		seen[id] = true
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE id=? AND role<>'superadmin' AND status='active' AND is_deleted=0`, id).Scan(&count); err != nil || count != 1 {
			return errors.New("参赛选手列表中存在无效账号")
		}
	}
	if _, err := tx.Exec(`DELETE FROM event_setup_roster WHERE event_id=?`, eventID); err != nil {
		return err
	}
	for position, id := range ids {
		side := strings.ToUpper(strings.TrimSpace(fixed[strconv.FormatInt(id, 10)]))
		if side != "A" && side != "B" {
			side = ""
		}
		if _, err := tx.Exec(`INSERT INTO event_setup_roster(event_id,user_id,fixed_team,position) VALUES(?,?,?,?)`, eventID, id, side, position); err != nil {
			return err
		}
	}
	return nil
}

type setupDrawStep struct {
	Kind     string  `json:"kind"`
	Position int     `json:"position,omitempty"`
	A        int64   `json:"a"`
	B        int64   `json:"b"`
	ExtraA   int64   `json:"extra_a,omitempty"`
	PoolA    []int64 `json:"pool_a"`
	PoolB    []int64 `json:"pool_b"`
}

// 后台一次确定全部结果并原子保存，动画计划仅随主动操作响应返回，SSE不补播。
func (a *App) drawEventSetup(c *gin.Context) {
	actor, id, access := a.managerEvent(c)
	if !access {
		return
	}
	steps := []setupDrawStep{}
	err := a.runMatchMutation(id, actor.ID, func(e *matchEngine) error {
		if err := requireEditableSettings(e.db, id); err != nil {
			return err
		}
		grouped, err := eventHasGrouping(e.db, id)
		if err != nil {
			return err
		}
		if grouped {
			return errors.New("已完成分组抽签，不能重复抽签")
		}
		rows, err := e.db.Query(`SELECT user_id,fixed_team FROM event_setup_roster WHERE event_id=? ORDER BY position,user_id`, id)
		if err != nil {
			return err
		}
		ids := []int64{}
		fixed := map[string]string{}
		for rows.Next() {
			var player int64
			var side string
			if err := rows.Scan(&player, &side); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, player)
			fixed[strconv.FormatInt(player, 10)] = side
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(ids) < 6 || len(ids) > 12 {
			return errors.New("分组抽签需要6到12名选手")
		}
		teams, err := e.teams()
		if err != nil {
			return err
		}
		if err := assignPlayers(e.db.(*sql.Tx), id, teams[0], teams[1], ids, fixed, e.rng); err != nil {
			return err
		}
		groups := [][]int64{}
		remaining := []int64{}
		for _, player := range ids {
			if fixed[strconv.FormatInt(player, 10)] == "" {
				remaining = append(remaining, player)
			}
		}
		for _, team := range teams {
			players, err := e.players(team)
			if err != nil {
				return err
			}
			if len(players) == 0 {
				return errors.New("两队都必须有选手")
			}
			e.rng.Shuffle(len(players), func(i, j int) { players[i], players[j] = players[j], players[i] })
			groups = append(groups, players)
		}
		// 固定成员直接可见，剩余成员按双方同时揭示；奇数末位与末轮一起进入A队。
		queues := [][]int64{{}, {}}
		for side, group := range groups {
			for _, player := range group {
				if containsID(remaining, player) {
					queues[side] = append(queues[side], player)
				}
			}
		}
		for len(queues[0])+len(queues[1]) > 0 {
			step := setupDrawStep{Kind: "group", PoolA: append([]int64{}, remaining...), PoolB: append([]int64{}, remaining...)}
			for side := 0; side < 2; side++ {
				if len(queues[side]) > 0 {
					player := queues[side][0]
					queues[side] = queues[side][1:]
					if side == 0 {
						step.A = player
					} else {
						step.B = player
					}
					for i, p := range remaining {
						if p == player {
							remaining = append(remaining[:i], remaining[i+1:]...)
							break
						}
					}
				}
			}
			if len(queues[0]) == 1 && len(queues[1]) == 0 && len(ids)%2 == 1 {
				step.ExtraA = queues[0][0]
				queues[0] = nil
				remaining = nil
			}
			steps = append(steps, step)
		}
		rules, err := e.rules()
		if err != nil {
			return err
		}
		if rules.Round1Mode == "random" {
			for pos := 0; pos < len(ids)/2; pos++ {
				step := setupDrawStep{Kind: "round1", Position: pos + 1, PoolA: append([]int64{}, groups[0][pos%len(groups[0]):]...), PoolB: append([]int64{}, groups[1][pos%len(groups[1]):]...)}
				for side, group := range groups {
					player := group[pos%len(group)]
					if side == 0 {
						step.A = player
					} else {
						step.B = player
					}
					if _, err := e.db.Exec(`INSERT INTO lineup_entries(event_id,team_id,position,player_id,source) VALUES(?,?,?,?,'setup_draw')`, id, teams[side], pos+1, player); err != nil {
						return err
					}
				}
				steps = append(steps, step)
			}
		}
		if rules.KOFStarter == "random" {
			step := setupDrawStep{Kind: "round2", Position: 1, A: groups[0][e.rng.Intn(len(groups[0]))], B: groups[1][e.rng.Intn(len(groups[1]))], PoolA: groups[0], PoolB: groups[1]}
			if _, err := e.db.Exec(`UPDATE events SET kof_initial_a=?,kof_initial_b=? WHERE id=?`, step.A, step.B, id); err != nil {
				return err
			}
			steps = append(steps, step)
		}
		if err := e.setStage(statusCaptainVote); err != nil {
			return err
		}
		return e.log("event_setup_drawn", steps)
	})
	if err != nil {
		fail(c, 400, err.Error())
		return
	}
	ok(c, gin.H{"steps": steps})
}
