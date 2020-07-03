package ot

import (
	"fmt"

	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/google/uuid"
)

type deltaWithClient struct {
	d        delta.Delta
	clientId *uuid.UUID
}

type File struct {
	id      uint32
	d       delta.Delta
	version uint32
	// Reverts serve 2 purposes:
	//
	// * Provide revert function
	// * Keep old versions of the document for slow clients
	reverts []deltaWithClient
	undos   uint32
	redos   uint32
}

func NewFile(id uint32, d delta.Delta) *File {
	return &File{
		id:      id,
		d:       d,
		version: 1,
		reverts: make([]deltaWithClient, 0),
	}
}

func (f *File) Content() ServerUpdate {
	return ServerUpdate{
		Id:      f.id,
		Base:    0,
		Version: f.version,
		Delta:   cloneDelta(&f.d),
	}
}

func sameClientId(clientId *uuid.UUID, data deltaWithClient) bool {
	return clientId != nil && data.clientId != nil && *clientId == *data.clientId
}

// This function would assume all changes submitted by the specified client has
// been applied, and send an update only contains changes from other users.
func (f *File) UpdateSince(clientId *uuid.UUID, base uint32) ServerUpdate {
	if base == 0 {
		return f.Content()
	}
	operations, baseContent, err := f.deltasSince(base)
	if err != nil {
		// Return full content when requested version is too old to track.
		return f.Content()
	}
	// TODO: unit test this, then optimize this
	allChanges := delta.New(nil)
	clientChanges := delta.New(nil)
	for _, opData := range operations {
		if sameClientId(clientId, opData) {
			d := allChanges.Transform(opData.d, false)
			d = clientChanges.Transform(*d, true)
			clientChanges = clientChanges.Compose(*d)
		}
		allChanges = allChanges.Compose(opData.d)
	}
	clientReverts := clientChanges.Invert(baseContent)
	d := clientReverts.Compose(*allChanges)

	return ServerUpdate{
		Id:      f.id,
		Base:    base,
		Version: f.version,
		Delta:   cloneDelta(d),
	}
}

func (f *File) Undo() error {
	undos, redos := f.undos, f.redos
	// If the last action performed is a redo, a new undo session will be started.
	// This is different from acme, and we might or might not revisit this later,
	// but for now, we will opt for the simple solution.
	if redos > 0 {
		undos, redos = 0, 0
	}
	skipItems := redos + undos*2
	if skipItems >= uint32(len(f.reverts)) {
		return fmt.Errorf("Running out of changes to undo!")
	}
	data := f.reverts[uint32(len(f.reverts))-1-skipItems]
	change := ClientChange{
		Id:    f.id,
		Delta: data.d,
		Base:  f.version,
	}
	_, err := f.Submit(nil, change)
	if err != nil {
		return err
	}
	f.undos, f.redos = undos+1, redos
	return nil
}

func (f *File) Redo() error {
	if f.redos >= f.undos {
		return fmt.Errorf("Running out of undos!")
	}
	data := f.reverts[uint32(len(f.reverts))-1-f.redos]
	change := ClientChange{
		Id:    f.id,
		Delta: data.d,
		Base:  f.version,
	}
	_, err := f.Submit(nil, change)
	if err != nil {
		return err
	}
	f.redos += 1
	return nil
}

func (f *File) Submit(clientId *uuid.UUID, change ClientChange) (ServerUpdate, error) {
	if change.Id != f.id {
		return ServerUpdate{}, fmt.Errorf("File ID does not match!")
	}
	if change.Base > f.version {
		return ServerUpdate{}, fmt.Errorf("Invalid change version %d, current version: %d", change.Base, f.version)
	} else if change.Base < f.version {
		operation, err := f.deltaSince(change.Base)
		if err != nil {
			return ServerUpdate{}, err
		}

		change.Delta = *operation.Transform(change.Delta, true)
		change.Base = f.version
	}
	revert := *change.Delta.Invert(&f.d)
	f.d = *f.d.Compose(change.Delta)
	f.version += 1
	f.reverts = append(f.reverts, deltaWithClient{
		d:        revert,
		clientId: clientId,
	})

	// New change will reset undo/redo session.
	f.undos, f.redos = 0, 0
	return ServerUpdate{
		Id:      f.id,
		Delta:   change.Delta,
		Base:    change.Base,
		Version: f.version,
	}, nil
}

func (f *File) deltasSince(base uint32) ([]deltaWithClient, *delta.Delta, error) {
	revertedVersions := int(f.version - base)
	if revertedVersions < 0 || revertedVersions > len(f.reverts) {
		return nil, nil, fmt.Errorf("Requested version %d is too old, oldest version now: %d", base, int(f.version)-len(f.reverts))
	}
	content := f.d
	deltas := make([]deltaWithClient, revertedVersions)
	for i := 0; i < revertedVersions; i++ {
		revertedData := f.reverts[len(f.reverts)-1-i]
		deltas[revertedVersions-1-i] = deltaWithClient{
			clientId: revertedData.clientId,
			d:        *revertedData.d.Invert(&content),
		}
		content = *content.Compose(revertedData.d)
	}
	return deltas, &content, nil
}

func (f *File) deltaSince(base uint32) (*delta.Delta, error) {
	deltas, _, err := f.deltasSince(base)
	if err != nil {
		return nil, err
	}
	current := delta.New(nil)
	for _, deltaData := range deltas {
		current = current.Compose(deltaData.d)
	}
	return current, nil
}

func cloneDelta(d *delta.Delta) delta.Delta {
	ops := make([]delta.Op, len(d.Ops))
	for i, op := range d.Ops {
		ops[i] = op
	}
	return *delta.New(ops)
}
