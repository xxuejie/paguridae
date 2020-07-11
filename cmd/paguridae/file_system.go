package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os/user"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"9fans.net/go/plan9"
	"xuejie.space/c/paguridae/pkg/ot"
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

func Start9PFileSystem(s *Session) error {
	currentUser, err := user.Current()
	if err != nil {
		return err
	}
	go func() {
	Loop:
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				default:
				case <-s.listenerSignal:
					// Normal exiting
					break Loop
				}
				log.Printf("Accepting error: %v", err)
				continue
			}
			go loop(s, conn, currentUser)
		}
	}()
	return nil
}

func loop(s *Session, conn net.Conn, currentUser *user.User) {
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
	openedFiles := make(map[uint32]bool)
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
			if err != io.EOF {
				log.Printf("Invalid message: %v", err)
			}
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
			delete(openedFiles, fcall.Fid)
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
			if fcall.Mode != plan9.OEXEC && (fcall.Mode&plan9.ORCLOSE == 0) {
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
				openedFiles[fcall.Fid] = true
				response.Type = plan9.Ropen
				response.Qid = qid
				response.Iounit = 0
			} else {
				response.Ename = "Invalid permission!"
			}
		case plan9.Tread:
			qid, ok := allocedFiles[fcall.Fid]
			if !ok {
				response.Ename = fmt.Sprintf("Fid %d is not assigned!", fcall.Fid)
				break
			}
			if !openedFiles[fcall.Fid] {
				response.Ename = fmt.Sprintf("Fid %d is not opened!", fcall.Fid)
				break
			}
			pathType := uint32(qid.Path) & PATH_TYPE_MASK
			var data []byte
			if qid.Type&plan9.QTDIR != 0 {
				t := time.Now().Unix()
				for path, fileinfo := range fileinfos {
					if path&PATH_TYPE_MASK == pathType {
						currentQid := plan9.Qid{
							Path: ((qid.Path >> 32) << 32) | uint64(path),
							Vers: qid.Vers,
							Type: fileinfo.Type,
						}
						data = append(data, generateStat(currentQid, fileinfo, currentUser, t)...)
					}
				}
				if pathType == PATH_TYPE_ROOT {
					// Insert current open files to ROOT folder
					files := &otFiles{}
					for _, change := range s.Server.AllContents() {
						if change.Id%2 != 0 {
							files.add(change)
						}
					}
					sort.Sort(files)
					for _, file := range files.files {
						fullQpath := uint64(PATH_TYPE_FILE) | (uint64(Q_DIR) << 8) | (uint64(file.Id) << 32)
						currentFileinfo := fileinfos[uint32(fullQpath)]
						qid := plan9.Qid{
							Path: fullQpath,
							Vers: file.Version,
							Type: currentFileinfo.Type,
						}
						data = append(data, generateStat(qid, fileinfo{
							Name: strconv.Itoa(int(file.Id)),
							Type: currentFileinfo.Type,
							Perm: currentFileinfo.Perm,
						}, currentUser, t)...)
					}
				}
				fillRreadData(data, *fcall, &response)
			} else {
				qType := uint8(qid.Path >> 8)
				if pathType == PATH_TYPE_ROOT {
					switch qType {
					case Q_ROOT_CONS:
						fillRreadData(data, *fcall, &response)
					case Q_ROOT_INDEX:
						files := &otFiles{}
						for _, change := range s.Server.AllContents() {
							if change.Id != 0 {
								files.add(change)
							}
						}
						sort.Sort(files)
						for i := 0; i+1 < len(files.files); {
							if files.files[i+1].Id != files.files[i].Id+1 {
								// This should be unlikely to occur
								i += 1
								continue
							}
							labelContent := DeltaToString(files.files[i].Delta, true)
							isDirectory := 0
							if m, _ := regexp.MatchString(`^[^ \n\|]+\/\s+\|`, labelContent); m {
								isDirectory = 1
							}
							changed := 0
							if m, _ := regexp.MatchString(`^(?:[^ \n\|]+\s+)?(\|\*)`, labelContent); m {
								changed = 1
							}
							var firstLine string
							lines := strings.Split(labelContent, "\n")
							if len(lines) > 0 {
								firstLine = lines[0]
							}
							output := fmt.Sprintf("%16d %16d %16d %16d %16d %s\n",
								files.files[i].Id,
								files.files[i].Delta.Length(),
								files.files[i+1].Delta.Length(),
								isDirectory,
								changed,
								firstLine,
							)
							data = append(data, []byte(output)...)
							i += 2
						}
						fillRreadData(data, *fcall, &response)
					}
				} else {
					fileId := uint32(qid.Path >> 32)
					switch qType {
					case Q_FILE_TAG:
						change := s.Server.Content(fileId)
						if change != nil {
							data = []byte(DeltaToString(change.Delta, true))
						}
						fillRreadData(data, *fcall, &response)
					case Q_FILE_BODY:
						change := s.Server.Content(fileId + 1)
						if change != nil {
							data = []byte(DeltaToString(change.Delta, true))
						}
						fillRreadData(data, *fcall, &response)
					}
				}
			}
		case plan9.Tstat:
			qid, ok := allocedFiles[fcall.Fid]
			if !ok {
				response.Ename = fmt.Sprintf("Fid %d is not assigned!", fcall.Fid)
				break
			}
			fileinfo := fileinfos[uint32(qid.Path)]
			response.Stat = generateStat(qid, fileinfo, currentUser, time.Now().Unix())
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
				qids, err := walk(qid, fcall.Wname, s)
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
		case plan9.Twrite:
			qid, ok := allocedFiles[fcall.Fid]
			if !ok {
				response.Ename = fmt.Sprintf("Fid %d is not assigned!", fcall.Fid)
				break
			}
			if !openedFiles[fcall.Fid] {
				response.Ename = fmt.Sprintf("Fid %d is not opened!", fcall.Fid)
				break
			}
			pathType := uint32(qid.Path) & PATH_TYPE_MASK
			qType := uint8(qid.Path >> 8)
			if pathType == PATH_TYPE_ROOT {
				if qType == Q_ROOT_CONS {
					_, err := s.newErrorBuffer(nil).Write(fcall.Data)
					if err != nil {
						response.Ename = fmt.Sprintf("Write error: %v", err)
					} else {
						s.Flush()
						response.Count = uint32(len(fcall.Data))
						response.Type = plan9.Rwrite
					}
				}
			} else {
				fileId := uint32(qid.Path >> 32)
				switch qType {
				case Q_FILE_ERRORS:
					_, err := s.newErrorBuffer(&fileId).Write(fcall.Data)
					if err != nil {
						response.Ename = fmt.Sprintf("Write error: %v", err)
					} else {
						s.Flush()
						response.Count = uint32(len(fcall.Data))
						response.Type = plan9.Rwrite
					}
				case Q_FILE_TAG:
					s.Server.Append(fileId, []rune(string(fcall.Data)))
					s.Flush()
					response.Count = uint32(len(fcall.Data))
					response.Type = plan9.Rwrite
				case Q_FILE_BODY:
					s.Server.Append(fileId+1, []rune(string(fcall.Data)))
					s.Flush()
					response.Count = uint32(len(fcall.Data))
					response.Type = plan9.Rwrite
				}
			}
		}
		err = plan9.WriteFcall(conn, &response)
		if err != nil {
			log.Printf("Error writing auth reply: %v", err)
			conn.Close()
			return
		}
	}
}

func walk(start plan9.Qid, wnames []string, s *Session) ([]plan9.Qid, error) {
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
				contentId, err := s.CreateDummyFile()
				if err != nil {
					return nil, err
				}
				s.Flush()
				labelId := contentId - 1
				p := uint64(PATH_TYPE_FILE) | (uint64(Q_DIR) << 8) | (uint64(labelId) << 32)
				fullQpath = &p
			}
		}
		if fullQpath != nil {
			if (*fullQpath)&PATH_TYPE_MASK == PATH_TYPE_FILE {
				fileId := uint32((*fullQpath) >> 32)
				// File IDs only include label IDs
				if fileId%2 != 0 {
					change := s.Server.Content(fileId)
					if change != nil {
						fileinfo := fileinfos[uint32(*fullQpath)]
						qid = &plan9.Qid{
							Path: *fullQpath,
							Vers: change.Version,
							Type: fileinfo.Type,
						}
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
						Path: uint64(qpath) | ((start.Path >> 32) << 32),
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

func fillRreadData(data []byte, fcall plan9.Fcall, response *plan9.Fcall) {
	if fcall.Offset < uint64(len(data)) {
		end := fcall.Offset + uint64(fcall.Count)
		if end > uint64(len(data)) {
			end = uint64(len(data))
		}
		response.Data = data[fcall.Offset:end]
	}
	response.Type = plan9.Rread
}

func generateStat(qid plan9.Qid, fileinfo fileinfo, currentUser *user.User, t int64) []byte {
	name := fileinfo.Name
	if name == "/" {
		name = "."
	}
	dir := plan9.Dir{
		Type:  uint16(fileinfo.Type),
		Dev:   0,
		Qid:   qid,
		Mode:  plan9.Perm(fileinfo.Perm),
		Atime: uint32(t),
		Mtime: uint32(t),
		// Right now we are copying plan9port's acme behavior
		Length: 0,
		Name:   name,
		Uid:    currentUser.Uid,
		Gid:    currentUser.Gid,
		Muid:   currentUser.Uid,
	}
	data, _ := dir.Bytes()
	return data
}

type otFiles struct {
	files []ot.ServerUpdate
}

func (f *otFiles) add(file ot.ServerUpdate) {
	f.files = append(f.files, file)
}

func (f *otFiles) Len() int {
	return len(f.files)
}

func (f *otFiles) Less(i, j int) bool {
	return f.files[i].Id < f.files[j].Id
}

func (f *otFiles) Swap(i, j int) {
	f.files[i], f.files[j] = f.files[j], f.files[i]
}
