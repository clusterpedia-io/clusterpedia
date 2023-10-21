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
