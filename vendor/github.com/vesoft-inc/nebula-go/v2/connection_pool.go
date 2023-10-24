/* Copyright (c) 2020 vesoft inc. All rights reserved.
 *
 * This source code is licensed under Apache 2.0 License,
 * attached with Common Clause Condition 1.0, found in the LICENSES directory.
 */

package nebula_go

import (
	"container/list"
	"fmt"
	"sync"
	"time"

	graph "github.com/vesoft-inc/nebula-go/v2/nebula/graph"
)

type ConnectionPool struct {
	idleConnectionQueue   list.List
	activeConnectionQueue list.List
	addresses             []HostAddress
	conf                  PoolConfig
	hostIndex             int
	log                   Logger
	rwLock                sync.RWMutex
}

func NewConnectionPool(addresses []HostAddress, conf PoolConfig, log Logger) (*ConnectionPool, error) {
	// Process domain to IP
	convAddress, err := DomainToIP(addresses)
	if err != nil {
		return nil, fmt.Errorf("Failed to find IP, error: %s ", err.Error())
	}

	// Check input
	if len(convAddress) == 0 {
		return nil, fmt.Errorf("Failed to initialize connection pool: illegal address input")
	}

	// Check config
	conf.validateConf(log)

	newPool := &ConnectionPool{
		conf:      conf,
		log:       log,
		addresses: convAddress,
		hostIndex: 0,
	}
	if err = newPool.initPool(); err != nil {
		return nil, err
	}
	return newPool, nil
}

func (pool *ConnectionPool) initPool() error {
	for i := 0; i < pool.conf.MinConnPoolSize; i++ {
		// Simple round-robin
		newConn := newConnection(pool.addresses[i%len(pool.addresses)])

		// Open connection to host
		err := newConn.open(newConn.severAddress, pool.conf.TimeOut)
		if err != nil {
			// If initialization failed, clean idle queue
			idleLen := pool.idleConnectionQueue.Len()
			for i := 0; i < idleLen; i++ {
				pool.idleConnectionQueue.Front().Value.(*connection).close()
				pool.idleConnectionQueue.Remove(pool.idleConnectionQueue.Front())
			}
			return fmt.Errorf("Failed to open connection, error: %s ", err.Error())
		}
		// Mark connection as in use
		pool.idleConnectionQueue.PushBack(newConn)
	}
	pool.log.Info("connection pool is initialized successfully")
	return nil
}

func (pool *ConnectionPool) GetSession(username, password string) (*Session, error) {
	// Get valid and usable connection
	var conn *connection = nil
	var err error = nil
	const retryTimes = 3
	for i := 0; i < retryTimes; i++ {
		conn, err = pool.getIdleConn()
		if err == nil {
			break
		}
	}
	if conn == nil {
		return nil, err
	}
	// Authenticate
	resp, err := conn.authenticate(username, password)
	if err != nil || resp.GetErrorCode() != graph.ErrorCode_SUCCEEDED {
		// if authentication failed, put connection back
		pool.rwLock.Lock()
		defer pool.rwLock.Unlock()
		removeFromList(&pool.activeConnectionQueue, conn)
		pool.idleConnectionQueue.PushBack(conn)
		return nil, err
	}

	sessID := resp.GetSessionID()
	// Create new session
	newSession := Session{
		sessionID:  sessID,
		connection: conn,
		connPool:   pool,
		log:        pool.log,
	}

	return &newSession, nil
}

func (pool *ConnectionPool) getIdleConn() (*connection, error) {
	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()

	// Take an idle valid connection if possible
	if pool.idleConnectionQueue.Len() > 0 {
		var newConn *connection = nil
		var newEle *list.Element = nil
		for ele := pool.idleConnectionQueue.Front(); ele != nil; ele = ele.Next() {
			// Check if connection is valid
			if res := ele.Value.(*connection).ping(); res {
				newConn = ele.Value.(*connection)
				newEle = ele
				break
			}
		}
		if newConn == nil {
			return pool.createConnection()
		}
		// Remove new connection from idle and add to active if found
		pool.idleConnectionQueue.Remove(newEle)
		pool.activeConnectionQueue.PushBack(newConn)
		return newConn, nil
	}

	// Create a new connection if there is no idle connection and total connection < pool max size
	newConn, err := pool.createConnection()
	// TODO: If no idle avaliable, wait for timeout and reconnect
	return newConn, err
}

// Release connection to pool
func (pool *ConnectionPool) release(conn *connection) {
	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()
	// Remove connection from active queue and add into idle queue
	removeFromList(&pool.activeConnectionQueue, conn)
	pool.idleConnectionQueue.PushBack(conn)
}

// Check avaliability of host
func (pool *ConnectionPool) Ping(host HostAddress, timeout time.Duration) error {
	newConn := newConnection(host)
	// Open connection to host
	if err := newConn.open(newConn.severAddress, timeout); err != nil {
		return err
	}
	newConn.close()
	return nil
}

// Close all connection
func (pool *ConnectionPool) Close() {
	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()
	idleLen := pool.idleConnectionQueue.Len()
	activeLen := pool.activeConnectionQueue.Len()

	for i := 0; i < idleLen; i++ {
		pool.idleConnectionQueue.Front().Value.(*connection).close()
		pool.idleConnectionQueue.Remove(pool.idleConnectionQueue.Front())
	}
	for i := 0; i < activeLen; i++ {
		pool.activeConnectionQueue.Front().Value.(*connection).close()
		pool.activeConnectionQueue.Remove(pool.activeConnectionQueue.Front())
	}
}

func (pool *ConnectionPool) getActiveConnCount() int {
	return pool.activeConnectionQueue.Len()
}

func (pool *ConnectionPool) getIdleConnCount() int {
	return pool.idleConnectionQueue.Len()
}

// Get a valid host (round robin)
func (pool *ConnectionPool) getHost() HostAddress {
	if pool.hostIndex == len(pool.addresses) {
		pool.hostIndex = 0
	}
	host := pool.addresses[pool.hostIndex]
	pool.hostIndex++
	return host
}

// Select a new host to create a new connection
func (pool *ConnectionPool) newConnToHost() (*connection, error) {
	// Get a valid host (round robin)
	host := pool.getHost()
	newConn := newConnection(host)
	// Open connection to host
	err := newConn.open(newConn.severAddress, pool.conf.TimeOut)
	if err != nil {
		return nil, err
	}
	// Add connection to active queue
	pool.activeConnectionQueue.PushBack(newConn)
	// TODO: update workload
	return newConn, nil
}

// Remove a connection from list
func removeFromList(l *list.List, conn *connection) {
	for ele := l.Front(); ele != nil; ele = ele.Next() {
		if ele.Value.(*connection) == conn {
			l.Remove(ele)
		}
	}
}

// Compare total connection number with pool max size and return a connection if capable
func (pool *ConnectionPool) createConnection() (*connection, error) {
	totalConn := pool.idleConnectionQueue.Len() + pool.activeConnectionQueue.Len()
	// If no idle avaliable and the number of total connection reaches the max pool size, return error/wait for timeout
	if totalConn >= pool.conf.MaxConnPoolSize {
		return nil, fmt.Errorf("Failed to get connection: No valid connection in the idle queue and connection number has reached the pool capacity")
	}

	newConn, err := pool.newConnToHost()
	if err != nil {
		return nil, err
	}
	// TODO: update workload
	return newConn, nil
}
