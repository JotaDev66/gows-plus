package main

import (
	"fmt"
	"go.mau.fi/whatsmeow/util/log"
	"net/http"
)

func StartPprofServer(log waLog.Logger, host string, port int) {
	addr := fmt.Sprintf("%s:%d", host, port)
	log.Infof("Starting pprof HTTP server on %s", addr)

	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Errorf("Failed to start pprof HTTP server: %v", err)
		}
	}()
}
