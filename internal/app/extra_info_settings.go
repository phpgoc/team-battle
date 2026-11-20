package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type extraInfoDefinition struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Options []string `json:"options"`
	IconURL string   `json:"icon_url,omitempty"`
}

func loadExtraInfoDefinitions(db sqlRunner) ([]extraInfoDefinition, error) {
	rows, err := db.Query(`SELECT id,name,options_json,CASE WHEN length(icon_data)>0 THEN '/api/extra-infos/' || id || '/icon' ELSE '' END FROM site_extra_infos ORDER BY sort_order,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []extraInfoDefinition{}
	for rows.Next() {
		var item extraInfoDefinition
		var options string
		if err := rows.Scan(&item.ID, &item.Name, &options, &item.IconURL); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(options), &item.Options); err != nil {
			return nil, fmt.Errorf("额外信息选项损坏，编号 %d: %w", item.ID, err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeExtraInfoOptions(options []string) ([]string, error) {
	if len(options) == 0 || len(options) > 50 {
		return nil, errors.New("额外信息需要1到50个选项")
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" || len([]rune(option)) > 100 || seen[option] {
			return nil, errors.New("额外信息选项不能为空、重复或超过100个字符")
		}
		seen[option] = true
		result = append(result, option)
	}
	return result, nil
}

func readExtraInfoDefinitionForm(c *gin.Context) (extraInfoDefinition, []byte, string, bool, error) {
	var result extraInfoDefinition
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 3<<20)
	if err := c.Request.ParseMultipartForm(3 << 20); err != nil {
		return result, nil, "", false, errors.New("额外信息格式不正确")
	}
	result.Name = strings.TrimSpace(c.PostForm("name"))
	removeIcon := c.PostForm("remove_icon") == "on"
	if result.Name == "" || len([]rune(result.Name)) > 80 {
		return result, nil, "", false, errors.New("额外信息名称不能为空且不能超过80个字符")
	}
	options, err := normalizeExtraInfoOptions(strings.FieldsFunc(c.PostForm("options"), func(char rune) bool { return char == '\n' || char == ',' || char == '\r' }))
	if err != nil {
		return result, nil, "", false, err
	}
	result.Options = options
	file, header, err := c.Request.FormFile("icon")
	if err != nil {
		if err == http.ErrMissingFile {
			return result, nil, "", removeIcon, nil
		}
		return result, nil, "", false, errors.New("额外信息图标无效")
	}
	defer file.Close()
	if header.Size > 2*1024*1024 {
		return result, nil, "", false, errors.New("额外信息图标不能超过2MB")
	}
	data, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil || len(data) == 0 || len(data) > 2*1024*1024 {
		return result, nil, "", false, errors.New("额外信息图标无效")
	}
	mimeType := http.DetectContentType(data)
	switch mimeType {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
	default:
		return result, nil, "", false, errors.New("额外信息图标只支持 JPG、PNG、GIF 或 WebP")
	}
	if removeIcon {
		return result, nil, "", false, errors.New("不能同时上传和清除图标")
	}
	return result, data, mimeType, false, nil
}

func (a *App) createExtraInfo(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	req, icon, iconMIME, _, err := readExtraInfoDefinitionForm(c)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	options, _ := json.Marshal(req.Options)
	if icon == nil {
		icon = []byte{}
	}
	if _, err := a.db.Exec(`INSERT INTO site_extra_infos(name,options_json,icon_data,icon_mime,sort_order) VALUES(?,?,?,?,COALESCE((SELECT MAX(sort_order)+1 FROM site_extra_infos),0))`, req.Name, string(options), icon, iconMIME); err != nil {
		fail(c, http.StatusBadRequest, "额外信息名称已存在或无法创建")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) updateExtraInfo(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("extraInfoID"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "额外信息编号无效")
		return
	}
	req, icon, iconMIME, removeIcon, err := readExtraInfoDefinitionForm(c)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	options, _ := json.Marshal(req.Options)
	query := `UPDATE site_extra_infos SET name=?,options_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`
	args := []any{req.Name, string(options), id}
	if removeIcon {
		query = `UPDATE site_extra_infos SET name=?,options_json=?,icon_data=X'',icon_mime='',updated_at=CURRENT_TIMESTAMP WHERE id=?`
	} else if len(icon) > 0 {
		query = `UPDATE site_extra_infos SET name=?,options_json=?,icon_data=?,icon_mime=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`
		args = []any{req.Name, string(options), icon, iconMIME, id}
	}
	result, err := a.db.Exec(query, args...)
	if err != nil {
		fail(c, http.StatusBadRequest, "额外信息名称已存在或无法修改")
		return
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		fail(c, http.StatusNotFound, "额外信息不存在")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) deleteExtraInfo(c *gin.Context) {
	if !a.requireSuperAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("extraInfoID"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "额外信息编号无效")
		return
	}
	used, err := extraInfoDefinitionUsed(a.db, id)
	if err != nil {
		fail(c, http.StatusInternalServerError, "检查赛事额外信息失败")
		return
	}
	if used {
		fail(c, http.StatusConflict, "已有赛事使用此额外信息，不能删除")
		return
	}
	if _, err := a.db.Exec(`DELETE FROM site_extra_infos WHERE id=?`, id); err != nil {
		fail(c, http.StatusInternalServerError, "删除额外信息失败")
		return
	}
	ok(c, gin.H{"ok": true})
}

func (a *App) extraInfoIcon(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "额外信息编号无效")
		return
	}
	var data []byte
	var mimeType string
	if err := a.db.QueryRow(`SELECT icon_data,icon_mime FROM site_extra_infos WHERE id=?`, id).Scan(&data, &mimeType); err != nil || len(data) == 0 {
		c.Status(http.StatusNotFound)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, mimeType, data)
}

func extraInfoDefinitionUsed(db sqlRunner, definitionID int64) (bool, error) {
	rows, err := db.Query(`SELECT rules_json FROM tournaments`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		var rules TournamentRules
		if err := json.Unmarshal([]byte(raw), &rules); err != nil {
			return false, err
		}
		for _, selections := range rules.ExtraInfo {
			for _, selection := range selections {
				if selection.DefinitionID == definitionID {
					return true, nil
				}
			}
		}
	}
	return false, rows.Err()
}

func normalizeTournamentExtraInfo(db sqlRunner, rules *TournamentRules, previous *TournamentRules) error {
	if len(rules.ExtraInfo) == 0 {
		rules.ExtraInfo = nil
		return nil
	}
	definitions, err := loadExtraInfoDefinitions(db)
	if err != nil {
		return err
	}
	byID := make(map[int64]extraInfoDefinition, len(definitions))
	for _, definition := range definitions {
		byID[definition.ID] = definition
	}
	previousByStage := map[string]map[int64]TournamentExtraInfo{}
	if previous != nil {
		for stage, selections := range previous.ExtraInfo {
			previousByStage[stage] = make(map[int64]TournamentExtraInfo, len(selections))
			for _, selection := range selections {
				previousByStage[stage][selection.DefinitionID] = selection
			}
		}
	}
	for stage, selections := range rules.ExtraInfo {
		if stage != stageRound1 && stage != stageRound2 && stage != stageAce {
			return errors.New("额外信息阶段无效")
		}
		seen := map[int64]bool{}
		for index := range selections {
			selection := &selections[index]
			if selection.DefinitionID <= 0 || seen[selection.DefinitionID] {
				return errors.New("额外信息类型无效或重复")
			}
			seen[selection.DefinitionID] = true
			if snapshot, exists := previousByStage[stage][selection.DefinitionID]; exists {
				selection.Name = snapshot.Name
				selection.Options = append([]string(nil), snapshot.Options...)
				selection.IconURL = snapshot.IconURL
			} else if definition, exists := byID[selection.DefinitionID]; exists {
				selection.Name = definition.Name
				selection.Options = definition.Options
				selection.IconURL = definition.IconURL
			} else {
				return errors.New("额外信息类型不存在")
			}
			if len(selection.Options) == 0 {
				return errors.New("额外信息没有可用选项")
			}
			allowed := make(map[string]bool, len(selection.Options))
			for _, option := range selection.Options {
				allowed[option] = true
			}
			selected := map[string]bool{}
			for selectedIndex, option := range selection.Selected {
				option = strings.TrimSpace(option)
				if !allowed[option] || selected[option] {
					return errors.New("额外信息随机池包含无效或重复选项")
				}
				selected[option] = true
				selection.Selected[selectedIndex] = option
			}
			sort.Strings(selection.Selected)
		}
		rules.ExtraInfo[stage] = selections
		if len(selections) == 0 {
			delete(rules.ExtraInfo, stage)
		}
	}
	if len(rules.ExtraInfo) == 0 {
		rules.ExtraInfo = nil
	}
	return nil
}

func (r TournamentRules) extraInfoForMatch(stage string, rng interface{ Intn(int) int }) (map[string]string, error) {
	selections := r.ExtraInfo[stage]
	result := make(map[string]string, len(selections))
	for _, selection := range selections {
		pool := selection.Selected
		if len(pool) == 0 {
			pool = selection.Options
		}
		if len(pool) == 0 {
			return nil, errors.New("额外信息没有可用随机选项")
		}
		result[selection.Name] = pool[rng.Intn(len(pool))]
	}
	return result, nil
}
