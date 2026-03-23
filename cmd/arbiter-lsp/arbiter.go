package main

import (
	arbiter "github.com/odvcencio/arbiter"
	"github.com/odvcencio/arbiter/explore"
)

func compileAndValidate(source []byte) error {
	_, err := arbiter.CompileFull(source)
	return err
}

func getSummary(source []byte) *explore.Summary {
	full, err := arbiter.CompileFull(source)
	if err != nil {
		return nil
	}
	return explore.BuildSummary(full.Program)
}
