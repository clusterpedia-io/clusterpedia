package norm

import (
	"github.com/zhihu/norm/dialectors"
)

// execute 真正执行一个 sql
func (db *DB) execute(sql string) (*dialectors.ResultSet, error) {
	tx := db.getInstance()
	// 指定 space
	tx.sql = sql
	if tx.debug {
		tx.logger.Info(tx.sql)
	}
	defer tx.teardown()

	result, err := tx.dialector.Execute(sql)
	if err != nil {
		return &dialectors.ResultSet{}, err
	}

	return result, nil
}
