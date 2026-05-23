//go:build !darwin

package main

import "io"

func maybeInstallApps(url string, data []byte, opts *Flags, output io.Writer) (bool, error) {
	return false, nil
}
