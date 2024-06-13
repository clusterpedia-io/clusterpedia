/*
 *
 * Copyright (c) 2022 vesoft inc. All rights reserved.
 *
 * This source code is licensed under Apache 2.0 License.
 *
 */

package nebula_go

import (
	"container/list"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/vesoft-inc/nebula-go/v3/nebula"
)

// SessionPool is a pool that manages sessions internally.
//
// Usage:
// Construct
// sessionPool = newSessionPool(conf)
//
// Initialize
// sessionPool.init()
//
// Execute query
// result = sessionPool.execute("query")
//
// Release:
// sessionPool.close()
//
// Notice that all queries will be executed in the default space specified in the pool config.
type SessionPool struct {
	idleSessions   list.List
	activeSessions list.List
	conf           SessionPoolConf
	tz             timezoneInfo
	log            Logger
	closed         bool
	cleanerChan    chan struct{} //notify when pool is close
	rwLock         sync.RWMutex
	sslConfig      *tls.Config
}

// NewSessionPool creates a new session pool with the given configs.
// There must be an existing SPACE in the DB.
func NewSessionPool(conf SessionPoolConf, log Logger) (*SessionPool, error) {
	// check the config
	conf.checkBasicFields(log)

	newSessionPool := &SessionPool{
		conf: conf,
		log:  log,
	}

	// init the pool
	if err := newSessionPool.init(); err != nil {
		return nil, fmt.Errorf("failed to create a new session pool, %s", err.Error())
	}
	newSessionPool.startCleaner()
	return newSessionPool, nil
}

// init initializes the session pool.
func (pool *SessionPool) init() error {
	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()
	// check the hosts status
	if err := checkAddresses(pool.conf.timeOut, pool.conf.serviceAddrs, pool.sslConfig); err != nil {
		return fmt.Errorf("failed to initialize the session pool, %s", err.Error())
	}

	// create sessions to fulfill the min pool size
	for i := 0; i < pool.conf.minSize; i++ {
		session, err := pool.newSession()
		if err != nil {
			return fmt.Errorf("failed to initialize the session pool, %s", err.Error())
		}

		session.returnedAt = time.Now()
		pool.addSessionToList(&pool.idleSessions, session)
	}

	return nil
}

// Execute returns the result of the given query as a ResultSet
// Notice there are some limitations:
// 1. The query should not be a plain space switch statement, e.g. "USE test_space",
// but queries like "use space xxx; match (v) return v" are accepted.
// 2. If the query contains statements like "USE <space name>", the space will be set to the
// one in the pool config after the execution of the query.
// 3. The query should not change the user password nor drop a user.
func (pool *SessionPool) Execute(stmt string) (*ResultSet, error) {
	return pool.ExecuteWithParameter(stmt, map[string]interface{}{})
}

// ExecuteWithParameter returns the result of the given query as a ResultSet
func (pool *SessionPool) ExecuteWithParameter(stmt string, params map[string]interface{}) (*ResultSet, error) {
	// Check if the pool is closed
	if pool.closed {
		return nil, fmt.Errorf("failed to execute: Session pool has been closed")
	}

	// Get a session from the pool
	session, err := pool.getIdleSession()
	if err != nil {
		return nil, err
	}

	// Parse params
	paramsMap, err := parseParams(params)
	if err != nil {
		return nil, err
	}

	// Execute the query
	resp, err := session.connection.executeWithParameter(session.sessionID, stmt, paramsMap)
	if err != nil {
		return nil, err
	}

	resSet, err := genResultSet(resp, session.timezoneInfo)
	if err != nil {
		return nil, err
	}

	// if the space was changed after the execution of the given query,
	// change it back to the default space specified in the pool config
	if resSet.GetSpaceName() != "" && resSet.GetSpaceName() != pool.conf.spaceName {
		err := pool.setSessionSpaceToDefault(session)
		if err != nil {
			return nil, err
		}
	}

	// Return the session to the idle list
	pool.returnSession(session)

	return resSet, err
}

// ExecuteJson returns the result of the given query as a json string
// Date and Datetime will be returned in UTC
//	JSON struct:
// {
//     "results":[
//         {
//             "columns":[
//             ],
//             "data":[
//                 {
//                     "row":[
//                         "row-data"
//                     ],
//                     "meta":[
//                         "metadata"
//                     ]
//                 }
//             ],
//             "latencyInUs":0,
//             "spaceName":"",
//             "planDesc ":{
//                 "planNodeDescs":[
//                     {
//                         "name":"",
//                         "id":0,
//                         "outputVar":"",
//                         "description":{
//                             "key":""
//                         },
//                         "profiles":[
//                             {
//                                 "rows":1,
//                                 "execDurationInUs":0,
//                                 "totalDurationInUs":0,
//                                 "otherStats":{}
//                             }
//                         ],
//                         "branchInfo":{
//                             "isDoBranch":false,
//                             "conditionNodeId":-1
//                         },
//                         "dependencies":[]
//                     }
//                 ],
//                 "nodeIndexMap":{},
//                 "format":"",
//                 "optimize_time_in_us":0
//             },
//             "comment ":""
//         }
//     ],
//     "errors":[
//         {
//       		"code": 0,
//       		"message": ""
//         }
//     ]
// }
func (pool *SessionPool) ExecuteJson(stmt string) ([]byte, error) {
	return pool.ExecuteJsonWithParameter(stmt, map[string]interface{}{})
}

// ExecuteJson returns the result of the given query as a json string
// Date and Datetime will be returned in UTC
// The result is a JSON string in the same format as ExecuteJson()
//TODO(Aiee) check the space name
func (pool *SessionPool) ExecuteJsonWithParameter(stmt string, params map[string]interface{}) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")

	// Get a session from the pool
	session, err := pool.getIdleSession()
	if err != nil {
		return nil, err
	}
	// check the session is valid
	if session.connection == nil {
		return nil, fmt.Errorf("failed to execute: Session has been released")
	}
	// parse params
	paramsMap, err := parseParams(params)
	if err != nil {
		return nil, err
	}

	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()
	resp, err := session.connection.ExecuteJsonWithParameter(session.sessionID, stmt, paramsMap)
	if err != nil {
		return nil, err
	}

	//TODO(Aiee) check the space name
	return resp, nil
}

// Close logs out all sessions and closes bonded connection.
func (pool *SessionPool) Close() {
	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()

	//TODO(Aiee) append 2 lists
	idleLen := pool.idleSessions.Len()
	activeLen := pool.activeSessions.Len()

	// iterate all sessions
	for i := 0; i < idleLen; i++ {
		session := pool.idleSessions.Front().Value.(*Session)
		if session.connection == nil {
			session.log.Warn("Session has been released")
		} else if err := session.connection.signOut(session.sessionID); err != nil {
			session.log.Warn(fmt.Sprintf("Sign out failed, %s", err.Error()))
		}
		// close connection
		session.connection.close()
		pool.idleSessions.Remove(pool.idleSessions.Front())
	}
	for i := 0; i < activeLen; i++ {
		session := pool.activeSessions.Front().Value.(*Session)
		if session.connection == nil {
			session.log.Warn("Session has been released")
		} else if err := session.connection.signOut(session.sessionID); err != nil {
			session.log.Warn(fmt.Sprintf("Sign out failed, %s", err.Error()))
		}
		// close connection
		session.connection.close()
		pool.activeSessions.Remove(pool.activeSessions.Front())
	}

	pool.closed = true
	if pool.cleanerChan != nil {
		close(pool.cleanerChan)
	}
}

// GetTotalSessionCount returns the total number of sessions in the pool
func (pool *SessionPool) GetTotalSessionCount() int {
	pool.rwLock.RLock()
	defer pool.rwLock.RUnlock()
	return pool.activeSessions.Len() + pool.idleSessions.Len()
}

// newSession creates a new session and returns it.
// `use <space>` will be executed so that the new session will be in the default space.
func (pool *SessionPool) newSession() (*Session, error) {
	graphAddr := pool.getNextAddr()
	cn := connection{
		severAddress: graphAddr,
		timeout:      0 * time.Millisecond,
		returnedAt:   time.Now(),
		sslConfig:    nil,
		graph:        nil,
	}

	// open a new connection
	if err := cn.open(cn.severAddress, pool.conf.timeOut, nil); err != nil {
		return nil, fmt.Errorf("failed to create a net.Conn-backed Transport,: %s", err.Error())
	}

	// authenticate with username and password to get a new session
	authResp, err := cn.authenticate(pool.conf.username, pool.conf.password)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new session: %s", err.Error())
	}

	// If the authentication failed, close the session pool because the pool must have a valid user to work
	if authResp.GetErrorCode() != 0 {
		if authResp.GetErrorCode() == nebula.ErrorCode_E_BAD_USERNAME_PASSWORD ||
			authResp.GetErrorCode() == nebula.ErrorCode_E_USER_NOT_FOUND {
			pool.Close()
			return nil, fmt.Errorf(
				"failed to authenticate the user, error code: %d, error message: %s, the pool has been closed",
				authResp.ErrorCode, authResp.ErrorMsg)
		}
		return nil, fmt.Errorf("failed to create a new session: %s", authResp.GetErrorMsg())
	}

	sessID := authResp.GetSessionID()
	timezoneOffset := authResp.GetTimeZoneOffsetSeconds()
	timezoneName := authResp.GetTimeZoneName()
	// Create new session
	newSession := Session{
		sessionID:    sessID,
		connection:   &cn,
		connPool:     nil,
		sessPool:     pool,
		log:          pool.log,
		timezoneInfo: timezoneInfo{timezoneOffset, timezoneName},
	}

	// Switch to the default space
	stmt := fmt.Sprintf("USE %s", pool.conf.spaceName)
	useSpaceResp, err := newSession.connection.execute(newSession.sessionID, stmt)
	if err != nil {
		return nil, err
	}

	if useSpaceResp.GetErrorCode() != nebula.ErrorCode_SUCCEEDED {
		newSession.connection.close()
		return nil, fmt.Errorf("failed to use space %s: %s",
			pool.conf.spaceName, useSpaceResp.GetErrorMsg())
	}
	return &newSession, nil
}

// getNextAddr returns the next address in the address list using simple round robin approach.
func (pool *SessionPool) getNextAddr() HostAddress {
	if pool.conf.hostIndex == len(pool.conf.serviceAddrs) {
		pool.conf.hostIndex = 0
	}
	host := pool.conf.serviceAddrs[pool.conf.hostIndex]
	pool.conf.hostIndex++
	return host
}

// getSession returns an available session.
// This method should move an available session to the active list and should be MT-safe.
func (pool *SessionPool) getIdleSession() (*Session, error) {
	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()
	// Get a session from the idle queue if possible
	if pool.idleSessions.Len() > 0 {
		session := pool.idleSessions.Front().Value.(*Session)
		pool.removeSessionFromList(&pool.idleSessions, session)
		pool.addSessionToList(&pool.activeSessions, session)
		return session, nil
	} else if pool.activeSessions.Len() < pool.conf.maxSize {
		// Create a new session if the total number of sessions is less than the max size
		session, err := pool.newSession()
		if err != nil {
			return nil, err
		}
		pool.addSessionToList(&pool.activeSessions, session)
		return session, nil
	}
	// There is no available session in the pool and the total session count has reached the limit
	return nil, fmt.Errorf("failed to get session: no session available in the" +
		" session pool and the total session count has reached the limit")
}

// startCleaner starts sessionCleaner if idleTime > 0.
func (pool *SessionPool) startCleaner() {
	if pool.conf.idleTime > 0 && pool.cleanerChan == nil {
		pool.cleanerChan = make(chan struct{}, 1)
		go pool.sessionCleaner()
	}
}

func (pool *SessionPool) sessionCleaner() {
	const minInterval = time.Minute

	d := pool.conf.idleTime

	if d < minInterval {
		d = minInterval
	}
	t := time.NewTimer(d)

	for {
		select {
		case <-t.C:
		case <-pool.cleanerChan: // pool was closed.
		}

		pool.rwLock.Lock()

		if pool.closed {
			pool.cleanerChan = nil
			pool.rwLock.Unlock()
			return
		}

		closing := pool.timeoutSessionList()

		//release expired session from the pool
		for _, session := range closing {
			if session.connection == nil {
				session.log.Warn("Session has been released")
				return
			}
			if err := session.connection.signOut(session.sessionID); err != nil {
				session.log.Warn(fmt.Sprintf("Sign out failed, %s", err.Error()))
			}
			// close connection
			session.connection.close()
		}
		pool.rwLock.Unlock()

		t.Reset(d)
	}
}

// timeoutSessionList returns a list of sessions that have been idle for longer than the idle time.
func (pool *SessionPool) timeoutSessionList() (closing []*Session) {
	if pool.conf.idleTime > 0 {
		expiredSince := time.Now().Add(-pool.conf.idleTime)
		var newEle *list.Element = nil

		maxCleanSize := pool.idleSessions.Len() + pool.activeSessions.Len() - pool.conf.minSize

		for ele := pool.idleSessions.Front(); ele != nil; {
			if maxCleanSize == 0 {
				return
			}

			newEle = ele.Next()
			// Check Session is expired
			if !ele.Value.(*Session).returnedAt.Before(expiredSince) {
				return
			}
			closing = append(closing, ele.Value.(*Session))
			pool.idleSessions.Remove(ele)
			ele = newEle
			maxCleanSize--
		}
	}
	return
}

// parseParams converts the params map to a map of nebula.Value
func parseParams(params map[string]interface{}) (map[string]*nebula.Value, error) {
	paramsMap := make(map[string]*nebula.Value)
	for k, v := range params {
		nv, err := value2Nvalue(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse params: %s", err.Error())
		}
		paramsMap[k] = nv
	}
	return paramsMap, nil
}

// removeSessionFromIdleList Removes a session from list
func (pool *SessionPool) removeSessionFromList(l *list.List, session *Session) {
	for ele := l.Front(); ele != nil; ele = ele.Next() {
		if ele.Value.(*Session) == session {
			l.Remove(ele)
		}
	}
}

func (pool *SessionPool) addSessionToList(l *list.List, session *Session) {
	l.PushBack(session)
}

// returnSession returns a session from active list to the idle list.
func (pool *SessionPool) returnSession(session *Session) {
	pool.rwLock.Lock()
	defer pool.rwLock.Unlock()
	pool.removeSessionFromList(&pool.activeSessions, session)
	pool.addSessionToList(&pool.idleSessions, session)
	session.returnedAt = time.Now()
}

func (pool *SessionPool) setSessionSpaceToDefault(session *Session) error {
	stmt := fmt.Sprintf("USE %s", pool.conf.spaceName)
	resp, err := session.connection.execute(session.sessionID, stmt)
	if err != nil {
		return err
	}
	// if failed to change back to the default space, send a warning log
	// and remove the session from the pool because it is malformed.
	if resp.ErrorCode != nebula.ErrorCode_SUCCEEDED {
		pool.log.Warn(fmt.Sprintf("failed to reset the space of the session: errorCode: %s, errorMsg: %s, session removed",
			resp.ErrorCode, resp.ErrorMsg))
		pool.removeSessionFromList(&pool.activeSessions, session)
	}
	return nil
}
