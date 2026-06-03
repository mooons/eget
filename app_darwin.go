//go:build darwin

package main

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type appBundle struct {
	Name   string
	Source string
}

func maybeInstallApps(url string, data []byte, opts *Flags, output io.Writer, sourceTime time.Time, hasSourceTime bool) (bool, error) {
	if !shouldAutoInstallApps(opts) || !isAppCapableAsset(url) {
		return false, nil
	}

	name := assetName(url)
	var (
		bundles []appBundle
		cleanup func()
		err     error
	)

	switch {
	case strings.HasSuffix(strings.ToLower(name), ".dmg"):
		bundles, cleanup, err = appBundlesFromDMG(name, data)
	case strings.HasSuffix(strings.ToLower(name), ".zip"):
		bundles, cleanup, err = appBundlesFromExpandedArchive(name, data, "zip")
	default:
		bundles, cleanup, err = appBundlesFromExpandedArchive(name, data, "tar")
	}
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return false, err
	}
	if len(bundles) == 0 {
		if strings.HasSuffix(strings.ToLower(name), ".dmg") {
			return false, fmt.Errorf("no app bundles found in %s", name)
		}
		return false, nil
	}
	if opts.UpgradeOnly && !hasSourceTime {
		return false, fmt.Errorf("cannot determine source timestamp for automatic app installation with --upgrade-only")
	}

	return true, installAppBundles(bundles, opts, output, sourceTime, hasSourceTime)
}

func appBundlesFromDMG(name string, data []byte) ([]appBundle, func(), error) {
	workdir, err := os.MkdirTemp("", "eget-dmg-*")
	if err != nil {
		return nil, nil, err
	}

	assetPath := filepath.Join(workdir, name)
	if err := os.WriteFile(assetPath, data, 0644); err != nil {
		os.RemoveAll(workdir)
		return nil, nil, err
	}

	mountPoint, err := attachDMG(assetPath)
	if err != nil {
		os.RemoveAll(workdir)
		return nil, nil, err
	}
	cleanup := func() {
		detachDMG(mountPoint)
		_ = os.RemoveAll(workdir)
	}

	bundles, err := discoverAppBundles(mountPoint)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return bundles, cleanup, nil
}

func appBundlesFromExpandedArchive(name string, data []byte, kind string) ([]appBundle, func(), error) {
	workdir, err := os.MkdirTemp("", "eget-app-*")
	if err != nil {
		return nil, nil, err
	}

	assetPath := filepath.Join(workdir, name)
	if err := os.WriteFile(assetPath, data, 0644); err != nil {
		os.RemoveAll(workdir)
		return nil, nil, err
	}

	expanded := filepath.Join(workdir, "expanded")
	if err := os.MkdirAll(expanded, 0755); err != nil {
		os.RemoveAll(workdir)
		return nil, nil, err
	}

	var cmd *exec.Cmd
	switch kind {
	case "zip":
		cmd = exec.Command("ditto", "-x", "-k", assetPath, expanded)
	default:
		cmd = exec.Command("/usr/bin/tar", "-xf", assetPath, "-C", expanded)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(workdir)
		return nil, nil, fmt.Errorf("expand %s: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}

	bundles, err := discoverAppBundles(expanded)
	if err != nil {
		os.RemoveAll(workdir)
		return nil, nil, err
	}
	return bundles, func() { _ = os.RemoveAll(workdir) }, nil
}

func discoverAppBundles(root string) ([]appBundle, error) {
	var bundles []appBundle
	err := filepath.WalkDir(root, func(cur string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".app") {
			bundles = append(bundles, appBundle{
				Name:   d.Name(),
				Source: cur,
			})
			return filepath.SkipDir
		}
		return nil
	})
	return bundles, err
}

func installAppBundles(bundles []appBundle, opts *Flags, output io.Writer, sourceTime time.Time, hasSourceTime bool) error {
	upgradeCandidates := 0

	for _, bundle := range bundles {
		dest, err := appInstallTarget(bundle.Name, opts, len(bundles))
		if err != nil {
			return err
		}

		fi, err := os.Stat(dest)
		exists := err == nil
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if exists {
			if opts.UpgradeOnly && !sourceTime.After(fi.ModTime()) {
				fmt.Fprintf(output, "Skipping `%s`: installed app is up to date\n", bundle.Name)
				continue
			}
			upgradeCandidates++
			if err := os.RemoveAll(dest); err != nil {
				return err
			}
		} else {
			upgradeCandidates++
		}

		if err := copyAppBundle(bundle.Source, dest); err != nil {
			return err
		}
		warnIfUntrusted(dest)
		if err := clearQuarantine(dest); err != nil {
			return err
		}
		if hasSourceTime {
			if err := os.Chtimes(dest, sourceTime, sourceTime); err != nil {
				return err
			}
		}
		fmt.Fprintf(output, "Installed `%s` to `%s`\n", bundle.Name, dest)
	}

	if opts.UpgradeOnly && upgradeCandidates == 0 {
		return ErrNoUpgrade
	}
	return nil
}

func appInstallTarget(name string, opts *Flags, count int) (string, error) {
	output := resolvedAppInstallOutput(opts)
	if output != "" && strings.HasSuffix(output, ".app") {
		if count != 1 {
			return "", fmt.Errorf("--to %s requires exactly one app bundle", output)
		}
		return output, nil
	}

	root := defaultAppInstallDir
	if output != "" {
		root = output
	}
	return filepath.Join(root, name), nil
}

func resolvedAppInstallOutput(opts *Flags) string {
	if opts.OutputExplicit && opts.Output != "" {
		return opts.Output
	}
	if opts.AppOutput != "" {
		return opts.AppOutput
	}
	return opts.Output
}

func copyAppBundle(src string, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	out, err := exec.Command("ditto", src, dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("copy app bundle: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func warnIfUntrusted(appPath string) {
	out, err := exec.Command("spctl", "-a", "-vv", appPath).CombinedOutput()
	if err == nil {
		return
	}

	fmt.Fprintf(os.Stderr, "warning: trust check failed for `%s`:\n%s\n", appPath, strings.TrimSpace(string(out)))
	if details, derr := exec.Command("codesign", "-dv", appPath).CombinedOutput(); derr == nil || len(details) > 0 {
		fmt.Fprintf(os.Stderr, "%s\n", strings.TrimSpace(string(details)))
	}
}

func clearQuarantine(appPath string) error {
	out, err := exec.Command("xattr", "-rd", "com.apple.quarantine", appPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("clear quarantine on `%s`: %w\n%s", appPath, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func attachDMG(assetPath string) (string, error) {
	out, err := exec.Command("hdiutil", "attach", assetPath, "-nobrowse", "-readonly").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("mount dmg `%s`: %w\n%s", assetPath, err, strings.TrimSpace(string(out)))
	}
	if mountPoint, ok := mountPointFromAttachOutput(string(out)); ok {
		return mountPoint, nil
	}
	return "", fmt.Errorf("mount dmg `%s`: no mount point found", assetPath)
}

func mountPointFromAttachOutput(out string) (string, bool) {
	var mountPoint string

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "/dev/") {
			continue
		}

		fields := strings.Split(line, "\t")
		for i := len(fields) - 1; i >= 0; i-- {
			field := strings.TrimSpace(fields[i])
			if strings.HasPrefix(field, "/") && !strings.HasPrefix(field, "/dev/") {
				mountPoint = field
				break
			}
		}
	}

	return mountPoint, mountPoint != ""
}

func detachDMG(mountPoint string) {
	if mountPoint == "" {
		return
	}
	if out, err := exec.Command("hdiutil", "detach", mountPoint).CombinedOutput(); err != nil {
		_, _ = exec.Command("hdiutil", "detach", "-force", mountPoint).CombinedOutput()
		_ = out
	}
}
