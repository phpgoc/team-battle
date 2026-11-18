package app

import "errors"

// 管理员必须显式指定，可选任意有效账号，包括超级管理员。
func validateTournamentManager(db sqlRunner, id int64) error {
	if id <= 0 {
		return errors.New("请选择赛事管理员")
	}
	var valid bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users WHERE id=? AND status='active' AND is_deleted=0)`, id).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errors.New("赛事管理员账号无效")
	}
	return nil
}
