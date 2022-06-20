package main

import (
	"net/http"
	"sync/atomic"
)

// prototype of healthcheck
var state atomic.Value

func init() {
	state.Store("starting-up")
}

func stateFunc(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(state.Load().(string)))
}
