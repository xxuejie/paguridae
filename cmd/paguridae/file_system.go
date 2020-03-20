package main

import (
	"log"
	"net"

	"aqwari.net/net/styx"
	"github.com/xxuejie/go-delta-ot/ot"
)

func Serve9PFileSystem(listener net.Listener, server *ot.MultiFileServer) error {
	h := styx.HandlerFunc(func(s *styx.Session) {
		for s.Next() {
			if !server.Running() {
				return
			}
			log.Printf("Server: %p, request: %v", server, *s)
		}
	})
	fileServer := styx.Server{
		Handler: h,
	}
	// When when the outside connection stops, it will close the listener here,
	// triggering the 9p file server to also stop
	return fileServer.Serve(listener)
}
