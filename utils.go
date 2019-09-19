package main

import (
	"github.com/fmpwizard/go-quilljs-delta/delta"
	"github.com/sergi/go-diff/diffmatchpatch"
)

func DeltaToString(d delta.Delta) string {
	result := make([]rune, 0)
	for _, op := range d.Ops {
		if op.Insert != nil {
			result = append(result, op.Insert...)
		}
	}
	return string(result)
}

func Diff(old delta.Delta, new delta.Delta) *delta.Delta {
	oldString := DeltaToString(old)
	newString := DeltaToString(new)
	result := delta.New(nil)
	diffs := diffmatchpatch.New().DiffMain(oldString, newString, false)
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
