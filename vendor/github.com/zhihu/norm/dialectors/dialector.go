/**
* package: dialectors
* file: dirlector.go
* author: wuzhensheng
* create: 2021-06-23 11:52:00
* description:
**/
package dialectors

import (
	"time"

	nebula "github.com/vesoft-inc/nebula-go/v2"
)

const (
	DefaultTimeout         = 60 * time.Second
	DefaultIdleTime        = 10 * time.Minute
	DefaultMaxConnPoolSize = 20
)

type (
	ResultSet struct {
		*nebula.ResultSet
	}
	// IDialector mock nebula's pool. 取名来自 gorm 的 Dialector.
	IDialector interface {
		Execute(stmt string) (*ResultSet, error)
		Close()
	}
	DialectorConfig struct {
		Username        string        `json:"username" yaml:"username"`
		Password        string        `json:"password" yaml:"password"`
		Space           string        `json:"space" yaml:"space"`
		Timeout         time.Duration `json:"timeout" yaml:"timeout"`
		IdleTime        time.Duration `json:"idle_time" yaml:"idle_time"`
		MaxConnPoolSize int           `json:"max_conn_pool_size" yaml:"max_conn_pool_size"`
		MinConnPoolSize int           `json:"min_conn_pool_size" yaml:"min_conn_pool_size"`
		Addresses       []string      `json:"addresses" yaml:"addresses"`
	}
)

func (config *DialectorConfig) LoadDefault() {
	if config.Timeout <= 0 {
		config.Timeout = DefaultTimeout
	}
	if config.IdleTime <= 0 {
		config.IdleTime = DefaultIdleTime
	}
	if config.MaxConnPoolSize <= 0 {
		config.MaxConnPoolSize = DefaultMaxConnPoolSize
	}
}
