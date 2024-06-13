/* Copyright (c) 2020 vesoft inc. All rights reserved.
 *
 * This source code is licensed under Apache 2.0 License,
 * attached with Common Clause Condition 1.0, found in the LICENSES directory.
 */

package nebula_go

import (
	"fmt"

	"github.com/facebook/fbthrift/thrift/lib/go/thrift"
	graph "github.com/vesoft-inc/nebula-go/v2/nebula/graph"
)

type Session struct {
	sessionID  int64
	connection *connection
	connPool   *ConnectionPool
	log        Logger
}

// unsupported
// func (session *Session) ExecuteJson(stmt string) (*graph.ExecutionResponse, error) {
// 	return session.graph.ExecuteJson(session.sessionID, []byte(stmt))
// }

// Execute() returns the result of given query as a ResultSet
func (session *Session) Execute(stmt string) (*ResultSet, error) {
	if session.connection == nil {
		return nil, fmt.Errorf("Faied to execute: Session has been released")
	}
	resp, err := session.connection.execute(session.sessionID, stmt)
	if err == nil {
		return genResultSet(resp), nil
	}
	// Reconnect only if the tranport is closed
	err2, ok := err.(thrift.TransportException)
	if !ok {
		return nil, err
	}
	if err2.TypeID() == thrift.END_OF_FILE {
		_err := session.reConnect()
		if _err != nil {
			session.log.Error(fmt.Sprintf("Failed to reconnect, %s", _err.Error()))
			return nil, _err
		}
		session.log.Info(fmt.Sprintf("Successfully reconnect to host: %s, port: %d",
			session.connection.severAddress.Host, session.connection.severAddress.Port))
		// Execute with the new connetion
		resp, err := session.connection.execute(session.sessionID, stmt)
		if err != nil {
			return nil, err
		}
		return genResultSet(resp), nil
	} else { // No need to reconnect
		session.log.Error(fmt.Sprintf("Error info: %s", err2.Error()))
		return nil, err2
	}
}

func (session *Session) reConnect() error {
	newconnection, err := session.connPool.getIdleConn()
	if err != nil {
		err = fmt.Errorf(err.Error())
		return err
	}

	// Release connection to pool
	session.connPool.release(session.connection)
	session.connection = newconnection
	return nil
}

// Logout and release connetion hold by session
func (session *Session) Release() {
	if session == nil {
		session.log.Warn("Session is nil, no need to release")
		return
	}
	if session.connection == nil {
		session.log.Warn("Session has been released")
		return
	}
	if err := session.connection.signOut(session.sessionID); err != nil {
		session.log.Warn(fmt.Sprintf("Sign out failed, %s", err.Error()))
	}
	// Release connection to pool
	session.connPool.release(session.connection)
	session.connection = nil
}

func IsError(resp *graph.ExecutionResponse) bool {
	return resp.GetErrorCode() != graph.ErrorCode_SUCCEEDED
}
