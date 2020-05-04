package main

import (
	"fmt"
	"log"
	"net"

	"9fans.net/go/plan9"
	"github.com/xxuejie/go-delta-ot/ot"
)

const (
	PATH_TYPE_ROOT = 0
	PATH_TYPE_FILE = 1
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
			// Tversion
			fcall, err := plan9.ReadFcall(c)
			if err != nil {
				log.Printf("Error reading version message: %v", err)
				c.Close()
				return
			}
			if fcall.Type != plan9.Tversion ||
				fcall.Version != "9P2000" {
				log.Printf("Invalid version message: %s", fcall)
				c.Close()
				return
			}
			err = plan9.WriteFcall(c, &plan9.Fcall{
				Type:    plan9.Rversion,
				Tag:     fcall.Tag,
				Msize:   fcall.Msize,
				Version: fcall.Version,
			})
			if err != nil {
				log.Printf("Error writing version reply: %v", err)
				c.Close()
				return
			}
			// Tauth
			fcall, err = plan9.ReadFcall(c)
			if err != nil {
				log.Printf("Error reading auth message: %v", err)
				c.Close()
				return
			}
			if fcall.Type != plan9.Tauth {
				log.Printf("Invalid auth message: %s", fcall)
				c.Close()
				return
			}
			openedFiles := make(map[uint32]plan9.Qid)
			openedFiles[fcall.Afid] = plan9.Qid{
				Path: PATH_TYPE_ROOT,
				Vers: 0,
				Type: plan9.QTDIR,
			}
			err = plan9.WriteFcall(c, &plan9.Fcall{
				Type: plan9.Rauth,
				Tag:  fcall.Tag,
				Aqid: openedFiles[fcall.Afid],
			})
			if err != nil {
				log.Printf("Error writing auth reply: %v", err)
				c.Close()
				return
			}
			for {
				fcall, err := plan9.ReadFcall(c)
				if err != nil {
					log.Printf("Invalid message: %v", err)
					c.Close()
					return
				}
				response := plan9.Fcall{
					Type:  plan9.Rerror,
					Tag:   fcall.Tag,
					Ename: "Unknown error",
				}
				switch fcall.Type {
				default:
					log.Printf("Unknown fcall: %s", fcall)
					response.Ename = fmt.Sprintf("Unknown fcall: %d", fcall.Type)
				case plan9.Tattach:
					qid, ok := openedFiles[fcall.Afid]
					if !ok {
						response.Ename = fmt.Sprintf("Afid %d is not assigned!", fcall.Afid)
						break
					}
					_, ok = openedFiles[fcall.Fid]
					if ok {
						response.Ename = fmt.Sprintf("Fid %d has already been used", fcall.Fid)
						break
					}
					openedFiles[fcall.Fid] = qid
					response.Type = plan9.Rattach
					response.Qid = qid
				case plan9.Tclunk:
					delete(openedFiles, fcall.Fid)
					response.Type = plan9.Rclunk
				}
				err = plan9.WriteFcall(c, &response)
				if err != nil {
					log.Printf("Error writing auth reply: %v", err)
					c.Close()
					return
				}
			}
		}(conn)
	}
	return nil
}
