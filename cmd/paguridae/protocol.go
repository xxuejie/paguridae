package main

import (
	"github.com/google/uuid"
	"xuejie.space/c/paguridae/pkg/ot"
)

type Range struct {
	Index  uint32 `json:"index"`
	Length uint32 `json:"length"`
}

type Selection struct {
	Id    uint32 `json:"id"`
	Range Range  `json:"range"`
}

type Size struct {
	Id     uint32 `json:"id"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
}

type Action struct {
	Id        uint32    `json:"id"`
	Type      string    `json:"type"`
	Index     uint32    `json:"index"`
	Command   string    `json:"command"`
	Selection Selection `json:"selection"`
}

func (a Action) LabelId() uint32 {
	return a.Id - 1 + a.Id%2
}

func (a Action) ContentId() uint32 {
	return a.LabelId() + 1
}

type InitRequest struct {
	SessionId *uuid.UUID `json:"session,omitempty"`
	ClientId  *uuid.UUID `json:"client,omitempty"`
}

type InitResponse struct {
	SessionId uuid.UUID `json:"session"`
	ClientId  uuid.UUID `json:"client"`
}

type Request struct {
	Changes []ot.ClientChange `json:"changes,omitempty"`
	Acks    map[uint32]uint32 `json:"acks,omitempty"`
	Sizes   []Size            `json:"sizes,omitempty"`
	Action  *Action           `json:"action,omitempty"`
}

type Update struct {
	Updates map[uint32]ot.ServerUpdate `json:"updates,omitempty"`
	Hashes  map[uint32]string          `json:"hashes,omitempty"`
}
