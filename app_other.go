//go:build !darwin

package main

import (
	"io"
	"time"
)

func maybeInstallApps(url string, data []byte, opts *Flags, output io.Writer, sourceTime time.Time, hasSourceTime bool) (bool, error) {
	return false, nil
}
