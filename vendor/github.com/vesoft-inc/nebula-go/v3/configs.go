/*
 *
 * Copyright (c) 2020 vesoft inc. All rights reserved.
 *
 * This source code is licensed under Apache 2.0 License.
 *
 */

package nebula_go

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"time"
)

// PoolConfig is the configs of connection pool
type PoolConfig struct {
	// Socket timeout and Socket connection timeout, unit: seconds
	TimeOut time.Duration
	// The idleTime of the connection, unit: seconds
	// If connection's idle time is longer than idleTime, it will be delete
	// 0 value means the connection will not expire
	IdleTime time.Duration
	// The max connections in pool for all addresses
	MaxConnPoolSize int
	// The min connections in pool for all addresses
	MinConnPoolSize int
}

// validateConf validates config
func (conf *PoolConfig) validateConf(log Logger) {
	if conf.TimeOut < 0 {
		conf.TimeOut = 0 * time.Millisecond
		log.Warn("Illegal Timeout value, the default value of 0 second has been applied")
	}
	if conf.IdleTime < 0 {
		conf.IdleTime = 0 * time.Millisecond
		log.Warn("Invalid IdleTime value, the default value of 0 second has been applied")
	}
	if conf.MaxConnPoolSize < 1 {
		conf.MaxConnPoolSize = 10
		log.Warn("Invalid MaxConnPoolSize value, the default value of 10 has been applied")
	}
	if conf.MinConnPoolSize < 0 {
		conf.MinConnPoolSize = 0
		log.Warn("Invalid MinConnPoolSize value, the default value of 0 has been applied")
	}
}

// GetDefaultConf returns the default config
func GetDefaultConf() PoolConfig {
	return PoolConfig{
		TimeOut:         0 * time.Millisecond,
		IdleTime:        0 * time.Millisecond,
		MaxConnPoolSize: 10,
		MinConnPoolSize: 0,
	}
}

// GetDefaultSSLConfig reads the files in the given path and returns a tls.Config object
func GetDefaultSSLConfig(rootCAPath, certPath, privateKeyPath string) (*tls.Config, error) {
	rootCA, err := openAndReadFile(rootCAPath)
	if err != nil {
		return nil, err
	}
	cert, err := openAndReadFile(certPath)
	if err != nil {
		return nil, err
	}
	privateKey, err := openAndReadFile(privateKeyPath)
	if err != nil {
		return nil, err
	}
	clientCert, err := tls.X509KeyPair(cert, privateKey)
	if err != nil {
		return nil, err
	}
	// parse root CA pem and add into CA pool
	// for self-signed cert, use the local cert as the root ca
	rootCAPool := x509.NewCertPool()
	ok := rootCAPool.AppendCertsFromPEM(rootCA)
	if !ok {
		return nil, fmt.Errorf("unable to append supplied cert into tls.Config, please make sure it is a valid certificate")
	}
	return &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      rootCAPool,
	}, nil
}

func openAndReadFile(path string) ([]byte, error) {
	// open file
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("unable to open file %s: %s", path, err)
	}
	// read file
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("unable to ReadAll file %s: %s", path, err)
	}
	return b, nil
}

// SessionPoolConf is the configs of a session pool
// Note that the space name is bound to the session pool for its lifetime
type SessionPoolConf struct {
	username     string        // username for authentication
	password     string        // password for authentication
	serviceAddrs []HostAddress // service addresses for session pool
	hostIndex    int           // index of the host in ServiceAddrs that the next new session will connect to
	spaceName    string        // The space name that all sessions in the pool are bound to
	sslConfig    *tls.Config   // Optional SSL config for the connection

	// Basic pool configs
	// Socket timeout and Socket connection timeout, unit: seconds
	timeOut time.Duration
	// The idleTime of the connection, unit: seconds
	// If connection's idle time is longer than idleTime, it will be delete
	// 0 value means the connection will not expire
	idleTime time.Duration
	// The max sessions in pool for all addresses
	maxSize int
	// The min sessions in pool for all addresses
	minSize int
}

type SessionPoolConfOption func(*SessionPoolConf)

// NewSessionPoolConfOpt creates a new NewSessionPoolConf with the provided options
func NewSessionPoolConf(
	username, password string,
	serviceAddrs []HostAddress,
	spaceName string, opts ...SessionPoolConfOption) (*SessionPoolConf, error) {
	// Set default values for basic pool configs
	newPoolConf := SessionPoolConf{
		username:     username,
		password:     password,
		serviceAddrs: serviceAddrs,
		spaceName:    spaceName,
		timeOut:      0 * time.Millisecond,
		idleTime:     0 * time.Millisecond,
		maxSize:      30,
		minSize:      1,
		hostIndex:    0,
	}

	// Iterate the given options and apply them to the config.
	for _, overwrite := range opts {
		overwrite(&newPoolConf)
	}

	if err := newPoolConf.checkMandatoryFields(); err != nil {
		return nil, err
	}
	return &newPoolConf, nil
}

func WithSSLConfig(sslConfig *tls.Config) SessionPoolConfOption {
	return func(conf *SessionPoolConf) {
		conf.sslConfig = sslConfig
	}
}

func WithTimeOut(timeOut time.Duration) SessionPoolConfOption {
	return func(conf *SessionPoolConf) {
		conf.timeOut = timeOut
	}
}

func WithIdleTime(idleTime time.Duration) SessionPoolConfOption {
	return func(conf *SessionPoolConf) {
		conf.idleTime = idleTime
	}
}

func WithMaxSize(maxSize int) SessionPoolConfOption {
	return func(conf *SessionPoolConf) {
		conf.maxSize = maxSize
	}
}

func WithMinSize(minSize int) SessionPoolConfOption {
	return func(conf *SessionPoolConf) {
		conf.minSize = minSize
	}
}

func (conf *SessionPoolConf) checkMandatoryFields() error {
	// Check mandatory fields
	if conf.username == "" {
		return fmt.Errorf("invalid session pool config: Username is empty")
	}
	if conf.password == "" {
		return fmt.Errorf("invalid session pool config: Password is empty")
	}
	if len(conf.serviceAddrs) == 0 {
		return fmt.Errorf("invalid session pool config: Service address is empty")
	}
	if conf.spaceName == "" {
		return fmt.Errorf("invalid session pool config: Space name is empty")
	}
	return nil
}

// checkBasicFields checks the basic fields of the config and
// sets a default value if the given field value is invalid
func (conf *SessionPoolConf) checkBasicFields(log Logger) {
	// Check pool related fields, use default value if the given value is invalid
	if conf.timeOut < 0 {
		conf.timeOut = 0 * time.Millisecond
		log.Warn("Illegal Timeout value, the default value of 0 second has been applied")
	}
	if conf.idleTime < 0 {
		conf.idleTime = 0 * time.Millisecond
		log.Warn("Invalid IdleTime value, the default value of 0 second has been applied")
	}
	if conf.maxSize < 1 {
		conf.maxSize = 10
		log.Warn("Invalid MaxSize value, the default value of 10 has been applied")
	}
	if conf.minSize < 0 {
		conf.minSize = 0
		log.Warn("Invalid MinSize value, the default value of 0 has been applied")
	}
}
