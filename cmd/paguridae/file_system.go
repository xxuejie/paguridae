package main

import (
	"fmt"
	"log"
	"net"
	"os/user"
	"strconv"
	"time"

	"9fans.net/go/plan9"
	"github.com/xxuejie/go-delta-ot/ot"
)

const (
	PATH_TYPE_MASK = 0x1
	PATH_TYPE_ROOT = 0x0
	PATH_TYPE_FILE = 0x1

	Q_ROOT_DIR       = 0x0
	Q_ROOT_CONS      = 0x1
	Q_ROOT_INDEX     = 0x2
	Q_ROOT_NEW       = 0x3
	Q_FILE_DIR       = 0x0
	Q_FILE_ADDR      = 0x1
	Q_FILE_BODY      = 0x2
	Q_FILE_CTL       = 0x3
	Q_FILE_DATA      = 0x4
	Q_FILE_ERRORS    = 0x5
	Q_FILE_EVENT     = 0x6
	Q_FILE_TAG       = 0x7
	Q_FILE_XDATA     = 0x8
	Q_FILE_RICH_BODY = 0x12
	Q_FILE_RICH_DATA = 0x14
)

type fileinfo struct {
	Name string
	Type uint8
	Perm uint32
}

var fileinfos = map[uint32]fileinfo{
	(PATH_TYPE_ROOT | (Q_ROOT_DIR << 8)): {
		Name: "/",
		Type: plan9.QTDIR,
		Perm: 0500 | plan9.DMDIR,
	},
	(PATH_TYPE_ROOT | (Q_ROOT_CONS << 8)): {
		Name: "cons",
		Type: plan9.QTFILE,
		Perm: 0600,
	},
	(PATH_TYPE_ROOT | (Q_ROOT_INDEX << 8)): {
		Name: "index",
		Type: plan9.QTFILE,
		Perm: 0400,
	},
	(PATH_TYPE_ROOT | (Q_ROOT_NEW << 8)): {
		Name: "new",
		Type: plan9.QTDIR,
		Perm: 0500 | plan9.DMDIR,
	},
	(PATH_TYPE_FILE | (Q_FILE_DIR << 8)): {
		Name: ".",
		Type: plan9.QTDIR,
		Perm: 0500 | plan9.DMDIR,
	},
	(PATH_TYPE_FILE | (Q_FILE_ADDR << 8)): {
		Name: "addr",
		Type: plan9.QTFILE,
		Perm: 0600,
	},
	(PATH_TYPE_FILE | (Q_FILE_BODY << 8)): {
		Name: "body",
		Type: plan9.QTAPPEND,
		Perm: 0600 | plan9.DMAPPEND,
	},
	(PATH_TYPE_FILE | (Q_FILE_CTL << 8)): {
		Name: "ctl",
		Type: plan9.QTFILE,
		Perm: 0600,
	},
	(PATH_TYPE_FILE | (Q_FILE_DATA << 8)): {
		Name: "data",
		Type: plan9.QTFILE,
		Perm: 0600,
	},
	(PATH_TYPE_FILE | (Q_FILE_ERRORS << 8)): {
		Name: "errors",
		Type: plan9.QTFILE,
		Perm: 0200,
	},
	(PATH_TYPE_FILE | (Q_FILE_EVENT << 8)): {
		Name: "event",
		Type: plan9.QTFILE,
		Perm: 0600,
	},
	(PATH_TYPE_FILE | (Q_FILE_TAG << 8)): {
		Name: "tag",
		Type: plan9.QTAPPEND,
		Perm: 0600 | plan9.DMAPPEND,
	},
	(PATH_TYPE_FILE | (Q_FILE_XDATA << 8)): {
		Name: "xdata",
		Type: plan9.QTFILE,
		Perm: 0600,
	},
	(PATH_TYPE_FILE | (Q_FILE_RICH_BODY << 8)): {
		Name: "rich_body",
		Type: plan9.QTAPPEND,
		Perm: 0600 | plan9.DMAPPEND,
	},
	(PATH_TYPE_FILE | (Q_FILE_RICH_DATA << 8)): {
		Name: "rich_data",
		Type: plan9.QTFILE,
		Perm: 0600,
	},
}

func Serve9PFileSystem(listener net.Listener, quitSignal chan bool, server *ot.MultiFileServer) error {
	user, err := user.Current()
	if err != nil {
		return err
	}
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
				case plan9.Tstat:
					qid, ok := openedFiles[fcall.Fid]
					if !ok {
						response.Ename = fmt.Sprintf("Fid %d is not assigned!", fcall.Fid)
						break
					}
					fileinfo := fileinfos[uint32(qid.Path)]
					t := time.Now().Unix()
					dir := plan9.Dir{
						Type:  uint16(fileinfo.Type),
						Dev:   0,
						Qid:   qid,
						Mode:  plan9.Perm(fileinfo.Perm),
						Atime: uint32(t),
						Mtime: uint32(t),
						// Right now we are copying plan9port's acme behavior
						Length: 0,
						Name:   fileinfo.Name,
						Uid:    user.Uid,
						Gid:    user.Gid,
						Muid:   user.Uid,
					}
					response.Stat, _ = dir.Bytes()
					response.Type = plan9.Rstat
				case plan9.Twalk:
					qid, ok := openedFiles[fcall.Fid]
					if !ok {
						response.Ename = fmt.Sprintf("Fid %d is not assigned!", fcall.Fid)
						break
					}
					_, ok = openedFiles[fcall.Newfid]
					if ok {
						response.Ename = fmt.Sprintf("Newfid %d has already been used", fcall.Newfid)
						break
					}
					saveNewfid := true
					if len(fcall.Wname) > 0 {
						qids := walk(qid, fcall.Wname, server)
						if len(qids) == 0 {
							response.Ename = fmt.Sprintf("Unable to walk to: %s", fcall.Wname[0])
							break
						}
						response.Wqid = qids
						if len(qids) != len(fcall.Wname) {
							saveNewfid = false
						} else {
							qid = qids[len(qids)-1]
						}
					}
					if saveNewfid {
						openedFiles[fcall.Newfid] = qid
					}
					response.Type = plan9.Rwalk
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

func walk(start plan9.Qid, wnames []string, server *ot.MultiFileServer) []plan9.Qid {
	results := make([]plan9.Qid, 0)
	for _, wname := range wnames {
		var qid *plan9.Qid
		if start.Path == (PATH_TYPE_ROOT | (Q_ROOT_DIR << 8)) {
			i, err := strconv.Atoi(wname)
			if err == nil {
				fileId := uint32(i)
				change := server.CurrentChange(fileId)
				if change != nil {
					qpath := uint32(PATH_TYPE_FILE | (Q_FILE_DIR << 8))
					fileinfo := fileinfos[qpath]
					qid = &plan9.Qid{
						Path: uint64(qpath) | (uint64(fileId) << 32),
						Vers: change.Change.Version,
						Type: fileinfo.Type,
					}
				}
			}
		}
		if qid == nil {
			for qpath, fileinfo := range fileinfos {
				if start.Path&PATH_TYPE_MASK == uint64(qpath)&PATH_TYPE_MASK &&
					wname == fileinfo.Name {
					qid = &plan9.Qid{
						Path: uint64(qpath),
						Vers: 0,
						Type: fileinfo.Type,
					}
					break
				}
			}
		}
		if qid == nil {
			return results
		}
		results = append(results, *qid)
		start = *qid
	}
	return results
}
