package main

import "sync"

type categoryInfo struct {
	ID   int64
	Name string
	Icon string
}

type categoryDirectoryCache struct {
	mu             sync.RWMutex
	loaded         bool
	byID           map[int64]categoryInfo
	orderedIDs     []int64
	userToCategory map[int64]int64
}

func (a *App) categoryInfo(id int64) (categoryInfo, bool) {
	if id == 0 {
		return categoryInfo{}, false
	}
	a.directory.mu.RLock()
	if a.directory.loaded {
		item, ok := a.directory.byID[id]
		a.directory.mu.RUnlock()
		return item, ok
	}
	a.directory.mu.RUnlock()
	if err := a.reloadCategoryDirectory(); err != nil {
		return categoryInfo{}, false
	}
	a.directory.mu.RLock()
	item, ok := a.directory.byID[id]
	a.directory.mu.RUnlock()
	return item, ok
}

func (a *App) reloadCategoryDirectory() error {
	rows, err := a.db.Query(`SELECT id,name,icon FROM categories ORDER BY sort_order,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	byID := map[int64]categoryInfo{}
	ordered := []int64{}
	for rows.Next() {
		var item categoryInfo
		if err := rows.Scan(&item.ID, &item.Name, &item.Icon); err != nil {
			return err
		}
		byID[item.ID] = item
		ordered = append(ordered, item.ID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	userRows, err := a.db.Query(`SELECT id,category_id FROM users WHERE category_id IS NOT NULL`)
	if err != nil {
		return err
	}
	defer userRows.Close()
	userToCategory := map[int64]int64{}
	for userRows.Next() {
		var userID, categoryID int64
		if err := userRows.Scan(&userID, &categoryID); err != nil {
			return err
		}
		userToCategory[userID] = categoryID
	}
	if err := userRows.Err(); err != nil {
		return err
	}
	a.directory.mu.Lock()
	a.directory.byID = byID
	a.directory.orderedIDs = ordered
	a.directory.userToCategory = userToCategory
	a.directory.loaded = true
	a.directory.mu.Unlock()
	return nil
}

func (a *App) invalidateCategoryDirectory() {
	a.directory.mu.Lock()
	a.directory.loaded = false
	a.directory.byID = nil
	a.directory.orderedIDs = nil
	a.directory.userToCategory = nil
	a.directory.mu.Unlock()
}

func (a *App) cachedCategories() ([]map[string]any, error) {
	a.directory.mu.RLock()
	loaded := a.directory.loaded
	ids := append([]int64(nil), a.directory.orderedIDs...)
	byID := a.directory.byID
	a.directory.mu.RUnlock()
	if !loaded {
		if err := a.reloadCategoryDirectory(); err != nil {
			return nil, err
		}
		a.directory.mu.RLock()
		ids = append([]int64(nil), a.directory.orderedIDs...)
		byID = a.directory.byID
		a.directory.mu.RUnlock()
	}
	items := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		item := byID[id]
		items = append(items, map[string]any{"id": item.ID, "name": item.Name, "icon": item.Icon})
	}
	return items, nil
}

func (a *App) populateCategory(user *User) {
	a.directory.mu.RLock()
	loaded := a.directory.loaded
	a.directory.mu.RUnlock()
	if !loaded {
		_ = a.reloadCategoryDirectory()
	}
	a.directory.mu.RLock()
	if user.CategoryID == 0 {
		user.CategoryID = a.directory.userToCategory[user.ID]
	}
	a.directory.mu.RUnlock()
	if item, ok := a.categoryInfo(user.CategoryID); ok {
		user.CategoryName = item.Name
		user.CategoryIcon = item.Icon
	}
}
