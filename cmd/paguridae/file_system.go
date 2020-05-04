package main

import (
	"log"
	"net"

	"9fans.net/go/plan9"
	"github.com/xxuejie/go-delta-ot/ot"
)

func Serve9PFileSystem(listener net.Listener, quitSignal chan bool, server *ot.MultiFileServer) error {
Loop:
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			default:
			case <-quitSignal:
				// Normal exiting
				break Loop
			}
			log.Printf("Accepting error: %v", err)
			continue
		}
		go func(c net.Conn) {
			for {
				fcall, err := plan9.ReadFcall(c)
				if err != nil {
					log.Printf("Invalid message: %v", err)
					return
				}
				log.Printf("Fcall: %s", fcall)
			}
		}(conn)
	}
	return nil
}
