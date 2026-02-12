package singleinstance

import "errors"

var ErrAlreadyRunning = errors.New("another instance of UPGO Node is already running")
