package ot

import (
	"time"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/google/uuid"
)

const (
	typeConnect     = 1
	typeDisconnect  = 2
	typeCreateFiles = 3
	typeCloseFiles  = 4
	typeContent     = 5
	typeAllContents = 6
	typeSubmit      = 7
	typeAck         = 8
	typeUpdate      = 9
	typeUpdateAll   = 10
	typeBroadcast   = 11
	typeUndo        = 12
	typeRedo        = 13
)

type command struct {
	t             uint
	clientId      *uuid.UUID
	events        chan Event
	contents      []delta.Delta
	fileIds       []uint32
	fileId        uint32
	changes       []ClientChange
	acks          map[uint32]uint32
	updates       chan []ServerUpdate
	fileIdChan    chan []uint32
	updateFunc    UpdateFunction
	updateAllFunc UpdateAllFunction
	errorChan     chan error
}

// Client data structure in a server's view
type client struct {
	acks   map[uint32]uint32
	last   map[uint32]uint32
	events chan Event
}

type disconnectedClient struct {
	client         *client
	disconnectedAt time.Time
}
