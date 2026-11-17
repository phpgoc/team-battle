package app

import (
	"database/sql"
	"strconv"
	"sync"
)

type categoryInfo struct {
	ID            int64
	Name, IconURL string
}

// 账号名称、头像地址与分类映射统一驻留内存，变更后整体失效。
type categoryDirectoryCache struct {
	mu         sync.RWMutex
	loaded     bool
	byID       map[int64]categoryInfo
	orderedIDs []int64
	users      map[int64]User
}

func (a *App) reloadCategoryDirectory() error {
	a.directory.mu.Lock()
	defer a.directory.mu.Unlock()
	if a.directory.loaded {
		return nil
	}
	rows, err := a.db.Query(`SELECT id,name,CASE WHEN length(icon_data)>0 THEN '/api/categories/' || id || '/icon?v=' || updated_at ELSE '' END FROM categories ORDER BY sort_order,id`)
	if err != nil {
		return err
	}
	cats := map[int64]categoryInfo{}
	ordered := []int64{}
	for rows.Next() {
		var item categoryInfo
		if err := rows.Scan(&item.ID, &item.Name, &item.IconURL); err != nil {
			rows.Close()
			return err
		}
		cats[item.ID] = item
		ordered = append(ordered, item.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = a.db.Query(`SELECT id,username,display_name,role,status,created_at,COALESCE(category_id,0),CASE WHEN length(avatar_data)>0 THEN 1 ELSE 0 END,avatar_version,COALESCE(display_name_changed_at,''),COALESCE(avatar_changed_at,''),is_deleted FROM users`)
	if err != nil {
		return err
	}
	users := map[int64]User{}
	for rows.Next() {
		var u User
		var has, version int
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Status, &u.CreatedAt, &u.CategoryID, &has, &version, &u.DisplayNameChangedAt, &u.AvatarChangedAt, &u.IsDeleted); err != nil {
			rows.Close()
			return err
		}
		if has != 0 {
			u.AvatarURL = "/api/users/" + strconv.FormatInt(u.ID, 10) + "/avatar?v=" + strconv.Itoa(version)
		}
		if cat, ok := cats[u.CategoryID]; ok {
			u.CategoryName = cat.Name
			u.CategoryIcon = cat.IconURL
		}
		users[u.ID] = u
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	a.directory.byID = cats
	a.directory.orderedIDs = ordered
	a.directory.users = users
	a.directory.loaded = true
	return nil
}
func (a *App) invalidateCategoryDirectory() {
	a.directory.mu.Lock()
	a.directory.loaded = false
	a.directory.mu.Unlock()
}
func (a *App) categoryInfo(id int64) (categoryInfo, bool) {
	if err := a.reloadCategoryDirectory(); err != nil {
		return categoryInfo{}, false
	}
	a.directory.mu.RLock()
	defer a.directory.mu.RUnlock()
	item, ok := a.directory.byID[id]
	return item, ok
}
func (a *App) cachedUser(id int64) (User, error) {
	if err := a.reloadCategoryDirectory(); err != nil {
		return User{}, err
	}
	a.directory.mu.RLock()
	defer a.directory.mu.RUnlock()
	u, ok := a.directory.users[id]
	if !ok {
		return User{}, sql.ErrNoRows
	}
	return u, nil
}
func (a *App) cachedCategories() ([]map[string]any, error) {
	if err := a.reloadCategoryDirectory(); err != nil {
		return nil, err
	}
	a.directory.mu.RLock()
	defer a.directory.mu.RUnlock()
	items := []map[string]any{}
	for _, id := range a.directory.orderedIDs {
		c := a.directory.byID[id]
		items = append(items, map[string]any{"id": c.ID, "name": c.Name, "icon_url": c.IconURL, "icon": c.IconURL})
	}
	return items, nil
}
func (a *App) populateCategory(u *User) {
	if cached, err := a.cachedUser(u.ID); err == nil {
		u.CategoryID = cached.CategoryID
		u.CategoryName = cached.CategoryName
		u.CategoryIcon = cached.CategoryIcon
		u.AvatarURL = cached.AvatarURL
	}
}

func (a *App) userDirectory() (map[int64]User, error) {
	if err := a.reloadCategoryDirectory(); err != nil {
		return nil, err
	}
	a.directory.mu.RLock()
	defer a.directory.mu.RUnlock()
	// 每次失效后替换整个映射，已返回的映射只读且不会再改变。
	return a.directory.users, nil
}
