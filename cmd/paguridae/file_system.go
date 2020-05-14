package main

import (
	"fmt"
	"log"
	"net"
	"os/user"
	"strconv"
	"time"

	"9fans.net/go/plan9"
)

const (
	PATH_TYPE_MASK = 0x1
	PATH_TYPE_ROOT = 0x0
	PATH_TYPE_FILE = 0x1

	Q_DIR            = 0x0
	Q_ROOT_CONS      = 0x1
	Q_ROOT_INDEX     = 0x2
	Q_ROOT_NEW       = 0x3
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
	Q_MASK           = 0xFF
)

type fileinfo struct {
	Name string
	Type uint8
	Perm uint32
}

var fileinfos = map[uint32]fileinfo{
	(PATH_TYPE_ROOT | (Q_DIR << 8)): {
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
	(PATH_TYPE_FILE | (Q_DIR << 8)): {
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

func Start9PFileSystem(c *Connection) error {
	currentUser, err := user.Current()
	if err != nil {
		return err
	}
	go func() {
	Loop:
		for {
			conn, err := c.listener.Accept()
			if err != nil {
				select {
				default:
				case <-c.listenerSignal:
					// Normal exiting
					break Loop
				}
				log.Printf("Accepting error: %v", err)
				continue
			}
			go loop(c, conn, currentUser)
		}
	}()
	return nil
}

func loop(c *Connection, conn net.Conn, currentUser *user.User) {
	// Tversion
	fcall, err := plan9.ReadFcall(conn)
	if err != nil {
		log.Printf("Error reading version message: %v", err)
		conn.Close()
		return
	}
	if fcall.Type != plan9.Tversion ||
		fcall.Version != "9P2000" {
		log.Printf("Invalid version message: %s", fcall)
		conn.Close()
		return
	}
	err = plan9.WriteFcall(conn, &plan9.Fcall{
		Type:    plan9.Rversion,
		Tag:     fcall.Tag,
		Msize:   fcall.Msize,
		Version: fcall.Version,
	})
	if err != nil {
		log.Printf("Error writing version reply: %v", err)
		conn.Close()
		return
	}
	// Tauth
	fcall, err = plan9.ReadFcall(conn)
	if err != nil {
		log.Printf("Error reading auth message: %v", err)
		conn.Close()
		return
	}
	if fcall.Type != plan9.Tauth {
		log.Printf("Invalid auth message: %s", fcall)
		conn.Close()
		return
	}
	allocedFiles := make(map[uint32]plan9.Qid)
	allocedFiles[fcall.Afid] = plan9.Qid{
		Path: PATH_TYPE_ROOT,
		Vers: 0,
		Type: plan9.QTDIR,
	}
	err = plan9.WriteFcall(conn, &plan9.Fcall{
		Type: plan9.Rauth,
		Tag:  fcall.Tag,
		Aqid: allocedFiles[fcall.Afid],
	})
	if err != nil {
		log.Printf("Error writing auth reply: %v", err)
		conn.Close()
		return
	}
	for {
		fcall, err := plan9.ReadFcall(conn)
		if err != nil {
			log.Printf("Invalid message: %v", err)
			conn.Close()
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
			qid, ok := allocedFiles[fcall.Afid]
			if !ok {
				response.Ename = fmt.Sprintf("Afid %d is not assigned!", fcall.Afid)
				break
			}
			_, ok = allocedFiles[fcall.Fid]
			if ok {
				response.Ename = fmt.Sprintf("Fid %d has already been used", fcall.Fid)
				break
			}
			allocedFiles[fcall.Fid] = qid
			response.Type = plan9.Rattach
			response.Qid = qid
		case plan9.Tclunk:
			delete(allocedFiles, fcall.Fid)
			response.Type = plan9.Rclunk
		case plan9.Topen:
			qid, ok := allocedFiles[fcall.Fid]
			if !ok {
				response.Ename = fmt.Sprintf("Fid %d is not assigned!", fcall.Fid)
				break
			}
			fileinfo := fileinfos[uint32(qid.Path)]
			accepted := false
			fcall.Mode &= ^uint8(plan9.OTRUNC | plan9.OCEXEC)
			if fcall.Mode&(plan9.OEXEC|plan9.ORCLOSE) == 0 {
				m := uint32(0)
				switch fcall.Mode {
				default:
				case plan9.OREAD:
					m = 0400
				case plan9.OWRITE:
					m = 0200
				case plan9.ORDWR:
					m = 0600
				}
				if m != 0 && fileinfo.Perm&(^uint32(plan9.DMDIR|plan9.DMAPPEND))&m == m {
					accepted = true
				}
			}
			if accepted {
				// Right now no operation is needed for opening a file
				response.Type = plan9.Ropen
				response.Qid = qid
				response.Iounit = 0
			} else {
				response.Ename = "Invalid permission!"
			}
		case plan9.Tstat:
			qid, ok := allocedFiles[fcall.Fid]
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
				Uid:    currentUser.Uid,
				Gid:    currentUser.Gid,
				Muid:   currentUser.Uid,
			}
			response.Stat, _ = dir.Bytes()
			response.Type = plan9.Rstat
		case plan9.Twalk:
			qid, ok := allocedFiles[fcall.Fid]
			if !ok {
				response.Ename = fmt.Sprintf("Fid %d is not assigned!", fcall.Fid)
				break
			}
			_, ok = allocedFiles[fcall.Newfid]
			if ok {
				response.Ename = fmt.Sprintf("Newfid %d has already been used", fcall.Newfid)
				break
			}
			saveNewfid := true
			if len(fcall.Wname) > 0 {
				qids, err := walk(qid, fcall.Wname, c)
				if err != nil {
					response.Ename = fmt.Sprintf("Error occurs in walk: %v", err)
					break
				}
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
				allocedFiles[fcall.Newfid] = qid
			}
			response.Type = plan9.Rwalk
		}
		err = plan9.WriteFcall(conn, &response)
		if err != nil {
			log.Printf("Error writing auth reply: %v", err)
			conn.Close()
			return
		}
	}
}

func walk(start plan9.Qid, wnames []string, c *Connection) ([]plan9.Qid, error) {
	results := make([]plan9.Qid, 0)
	for _, wname := range wnames {
		var qid *plan9.Qid
		var fullQpath *uint64
		if wname == ".." {
			p := uint64(PATH_TYPE_ROOT)
			if start.Path&PATH_TYPE_MASK == PATH_TYPE_FILE {
				if (start.Path>>8)&Q_MASK != Q_DIR {
					p = start.Path&(^(uint64(Q_MASK) << 8)) | (Q_DIR << 8)
				}
			}
			fullQpath = &p
		} else if wname == "." {
			fullQpath = &start.Path
		} else if start.Path == (PATH_TYPE_ROOT | (Q_DIR << 8)) {
			i, err := strconv.Atoi(wname)
			if err == nil {
				p := uint64(PATH_TYPE_FILE) | (uint64(Q_DIR) << 8) | (uint64(i) << 32)
				fullQpath = &p
			} else if wname == "new" {
				i, err := c.CreateDummyFile()
				if err != nil {
					return nil, err
				}
				p := uint64(PATH_TYPE_FILE) | (uint64(Q_DIR) << 8) | (uint64(i) << 32)
				fullQpath = &p
			}
		}
		if fullQpath != nil {
			if (*fullQpath)&PATH_TYPE_MASK == PATH_TYPE_FILE {
				fileId := uint32((*fullQpath) >> 32)
				change := c.Server.CurrentChange(fileId)
				if change != nil {
					fileinfo := fileinfos[uint32(*fullQpath)]
					qid = &plan9.Qid{
						Path: *fullQpath,
						Vers: change.Change.Version,
						Type: fileinfo.Type,
					}
				}
			} else {
				fileinfo := fileinfos[uint32(*fullQpath)]
				qid = &plan9.Qid{
					Path: *fullQpath,
					Vers: 0,
					Type: fileinfo.Type,
				}
			}
		}
		if qid == nil {
			for qpath, fileinfo := range fileinfos {
				if start.Path&PATH_TYPE_MASK == uint64(qpath)&PATH_TYPE_MASK &&
					(start.Path>>8)&Q_MASK == Q_DIR &&
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
			return results, nil
		}
		results = append(results, *qid)
		start = *qid
	}
	return results, nil
}
