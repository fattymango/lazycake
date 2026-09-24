package api

import "errors"

var (
	ErrNotConnected = errors.New("api: node not connected")
	ErrBackpressure = errors.New("api: send buffer full")
)
