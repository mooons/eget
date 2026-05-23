package main

import (
	"os"
	"path"
	"runtime"
	"strings"
)

var (
	defaultAppInstallDir = "/Applications"
	userSelectFunc       = userSelect
	stdinInteractiveFunc = stdinInteractive
)

func stdinInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func shouldAutoInstallApps(opts *Flags) bool {
	return runtime.GOOS == "darwin" &&
		!opts.DLOnly &&
		opts.ExtractFile == "" &&
		opts.Output != "-"
}

func assetName(raw string) string {
	raw = strings.SplitN(raw, "?", 2)[0]
	return path.Base(raw)
}

func isAppCapableAsset(raw string) bool {
	name := strings.ToLower(assetName(raw))
	switch {
	case strings.HasSuffix(name, ".dmg"),
		strings.HasSuffix(name, ".zip"),
		strings.HasSuffix(name, ".tar.gz"),
		strings.HasSuffix(name, ".tgz"),
		strings.HasSuffix(name, ".tar.bz2"),
		strings.HasSuffix(name, ".tbz"),
		strings.HasSuffix(name, ".tar.xz"),
		strings.HasSuffix(name, ".txz"),
		strings.HasSuffix(name, ".tar.zst"),
		strings.HasSuffix(name, ".tar"):
		return true
	default:
		return false
	}
}

func preferDarwinAppAsset(candidates []string, opts *Flags) (string, bool) {
	if runtime.GOOS != "darwin" || opts.DLOnly || opts.ExtractFile != "" || len(opts.Asset) != 0 {
		return "", false
	}

	var dmgs []string
	for _, candidate := range candidates {
		if strings.HasSuffix(strings.ToLower(assetName(candidate)), ".dmg") {
			dmgs = append(dmgs, candidate)
		}
	}
	if len(dmgs) == 1 {
		return dmgs[0], true
	}
	return "", false
}
