package main

import (
	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func DeltaToRunes(d delta.Delta) []rune {
	result := make([]rune, 0)
	for _, op := range d.Ops {
		if op.Insert != nil {
			result = append(result, op.Insert...)
		}
	}
	return result
}

func Diff(old delta.Delta, new delta.Delta) *delta.Delta {
	oldRunes := DeltaToRunes(old)
	newRunes := DeltaToRunes(new)
	result := delta.New(nil)
	diffs := diffmatchpatch.New().DiffMainRunes(oldRunes, newRunes, false)
	for _, diff := range diffs {
		switch diff.Type {
		case diffmatchpatch.DiffDelete:
			result.Delete(len([]rune(diff.Text)))
		case diffmatchpatch.DiffInsert:
			result.Insert(diff.Text, nil)
		case diffmatchpatch.DiffEqual:
			result.Retain(len([]rune(diff.Text)), nil)
		}
	}
	return result
}
