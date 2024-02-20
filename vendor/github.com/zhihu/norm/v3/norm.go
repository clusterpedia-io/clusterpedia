package norm

import (
	"github.com/pkg/errors"
	"github.com/zhihu/norm/dialectors"
)

var teardown = func() {}

type DB struct {
	dialector dialectors.IDialector
	logger    Logger

	// 一次查询所需的字段
	parent *DB
	debug  bool
	sql    string

	// 一次查询结束后需要执行的操作
	teardown func()
}

// Open initialize nebula connect pool
func Open(dialector dialectors.IDialector, cfg Config, opts ...Option) (*DB, error) {
	if dialector == nil {
		return &DB{}, errors.New("must have dialector")
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg.LoadDefault()
	return &DB{
		dialector: dialector,
		logger:    cfg.logger,
		parent:    nil,
		debug:     cfg.DebugMode,
		teardown:  teardown,
	}, nil
}

// MustOpen 语法糖. 如果 error 则 panic, 适用于强依赖 db 的场景
func MustOpen(dialector dialectors.IDialector, cfg Config, opts ...Option) *DB {
	db, err := Open(dialector, cfg, opts...)
	if err != nil {
		panic(err)
	}
	return db
}

// Close
func (db *DB) Close() {
	db.dialector.Close()
}

// DebugMode 调试模式, 会输出任意语句的sql
func (db *DB) DebugMode() {
	db.debug = true
}

// getInstance 获取当前调用链真正使用的 db. 专为链式调用设计, 为每条调用链返回一个 db 的拷贝, 从而避免互相影响.
func (db *DB) getInstance() (tx *DB) {
	if db.parent == nil {
		tx = &DB{
			dialector: db.dialector,
			logger:    db.logger,
			parent:    db,
			debug:     db.debug,
			teardown:  teardown,
		}
		return tx
	}
	return db
}
