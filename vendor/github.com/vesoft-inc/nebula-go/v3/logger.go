/*
 *
 * Copyright (c) 2020 vesoft inc. All rights reserved.
 *
 * This source code is licensed under Apache 2.0 License.
 *
 */

package nebula_go

import (
	"log"
)

type Logger interface {
	Info(msg string)
	Warn(msg string)
	Error(msg string)
	Fatal(msg string)
}

type DefaultLogger struct{}

func (l DefaultLogger) Info(msg string) {
	log.Printf("[INFO] %s\n", msg)
}

func (l DefaultLogger) Warn(msg string) {
	log.Printf("[WARNING] %s\n", msg)
}

func (l DefaultLogger) Error(msg string) {
	log.Printf("[ERROR] %s\n", msg)
}

func (l DefaultLogger) Fatal(msg string) {
	log.Fatalf("[FATAL] %s\n", msg)
}
