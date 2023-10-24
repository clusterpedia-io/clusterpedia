package norm

import (
	"fmt"
	"strings"

	"github.com/zhihu/norm/constants"
)

// Debug will print the nGql when it exec
func (db *DB) Debug() (tx *DB) {
	tx = db.getInstance()
	tx.debug = true
	return
}

func (db *DB) Go(step int) (tx *DB) {
	tx = db.getInstance()

	if step > 1 {
		tx.sql += fmt.Sprintf("go %d step ", step)
	}

	return
}

func (db *DB) From(vs ...IVertex) (tx *DB) {
	tx = db.getInstance()

	vids := make([]string, len(vs))
	for i, v := range vs {
		vids[i] = GetVidWithPolicy(v.GetVid(), v.GetPolicy())
	}

	if tx.sql == "" {
		tx.sql += "go "
	}
	tx.sql += fmt.Sprintf("from %s ", strings.Join(vids, ","))

	return
}

func (db *DB) Over(edges ...IEdge) (tx *DB) {
	tx = db.getInstance()
	names := make([]string, len(edges))
	for i, edge := range edges {
		names[i] = edge.EdgeName()
	}
	sql := strings.Join(names, ",")
	tx.sql += fmt.Sprintf("over %s ", sql)
	return
}

// Reversely over egde reversely
func (db *DB) Reversely() (tx *DB) {
	tx = db.getInstance()
	tx.sql += constants.DirectionReversely + " "
	return
}

// Bidirect over egde bidirect
func (db *DB) Bidirect() (tx *DB) {
	tx = db.getInstance()
	tx.sql += constants.DirectionBidirect + " "
	return
}

func (db *DB) Limit(limit int) (tx *DB) {
	tx = db.getInstance()
	tx.sql += fmt.Sprintf("|limit %d ", limit)
	return
}

func (db *DB) Where(sql string) (tx *DB) {
	tx = db.getInstance()
	tx.sql += fmt.Sprintf("where %s ", sql)
	return
}

func (db *DB) Yield(sql string) (tx *DB) {
	tx = db.getInstance()
	tx.sql += fmt.Sprintf("yield %s ", sql)
	return
}

func (db *DB) Group(fields ...string) (tx *DB) {
	tx = db.getInstance()
	for i := range fields {
		fields[i] = "$-." + fields[i]
	}
	sql := strings.Join(fields, ",")
	tx.sql += fmt.Sprintf("|group by %s ", sql)
	return
}
