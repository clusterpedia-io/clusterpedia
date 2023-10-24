/**
* package: dialectors
* file: dirlector.go
* author: wuzhensheng
* create: 2021-06-23 11:52:00
* description:
**/
package dialectors

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	nebula "github.com/vesoft-inc/nebula-go/v2"
)

type (
	NebulaDialector struct {
		npool    *nebula.ConnectionPool
		username string
		password string
		space    string
	}
)

var _ IDialector = new(NebulaDialector)

func NewNebulaDialector(cfg DialectorConfig) (*NebulaDialector, error) {
	cfg.LoadDefault()
	nAddresses, err := parseAddresses(cfg.Addresses)
	if err != nil {
		return &NebulaDialector{}, err
	}
	nConfig := nebula.PoolConfig{
		TimeOut:         cfg.Timeout,
		IdleTime:        cfg.IdleTime,
		MaxConnPoolSize: cfg.MaxConnPoolSize,
		MinConnPoolSize: cfg.MinConnPoolSize,
	}
	nPool, err := nebula.NewConnectionPool(nAddresses, nConfig, nebula.DefaultLogger{})
	if err != nil {
		return &NebulaDialector{}, errors.Wrap(err, "connect nebula fail")
	}
	return &NebulaDialector{
		npool:    nPool,
		username: cfg.Username,
		password: cfg.Password,
		space:    cfg.Space,
	}, nil
}

// MustNewNebulaDialector 语法糖, 必须新建一个 Nebula Dialector
func MustNewNebulaDialector(cfg DialectorConfig) *NebulaDialector {
	dialector, err := NewNebulaDialector(cfg)
	if err != nil {
		panic(err)
	}
	return dialector
}

// Execute TODO 可以缓存一个 session pool.
func (d *NebulaDialector) Execute(stmt string) (*ResultSet, error) {
	session, err := d.getSession()
	if err != nil {
		return &ResultSet{}, err
	}
	defer session.Release()

	// TODO (nebula bug) 除了 root 用户外, nebula 不支持其他用户 ("use %s; %s", space, sql) 这种方式.
	// sql = fmt.Sprintf("use %s; %s", space, sql)
	_, err = session.Execute("use " + d.space)
	if err != nil {
		return &ResultSet{}, err
	}

	result, err := session.Execute(stmt)
	if err != nil {
		return &ResultSet{}, err
	}
	if err = checkResultSet(result); err != nil {
		return &ResultSet{}, err
	}

	return &ResultSet{result}, nil
}

func (d *NebulaDialector) getSession() (*nebula.Session, error) {
	return d.npool.GetSession(d.username, d.password)
}

func (d *NebulaDialector) Close() {
	d.npool.Close()
}

// checkResultSet 检查是否成功执行
func checkResultSet(nSet *nebula.ResultSet) error {
	if nSet.GetErrorCode() != nebula.ErrorCode_SUCCEEDED {
		return errors.New(fmt.Sprintf("code: %d, msg: %s",
			nSet.GetErrorCode(), nSet.GetErrorMsg()))
	}
	return nil
}

// parseAddresses 解析传入的 Host 格式为 nebula 需要的格式
func parseAddresses(addresses []string) ([]nebula.HostAddress, error) {
	hostAddresses := make([]nebula.HostAddress, len(addresses))
	for i, addr := range addresses {
		list := strings.Split(addr, ":")
		if len(list) < 2 {
			return []nebula.HostAddress{},
				errors.New(fmt.Sprintf("address %s invalid", addr))
		}
		port, err := strconv.ParseInt(list[1], 10, 64)
		if err != nil {
			return []nebula.HostAddress{},
				errors.New(fmt.Sprintf("address %s invalid", addr))
		}
		hostAddresses[i] = nebula.HostAddress{
			Host: list[0],
			Port: int(port),
		}
	}
	return hostAddresses, nil
}
