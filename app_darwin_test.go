//go:build darwin

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMaybeInstallAppsZipInstallsAppAndClearsQuarantine(t *testing.T) {
	srcRoot := t.TempDir()
	appPath := makeTestApp(t, srcRoot, "ZipTest", "v1", false, true)
	zipPath := filepath.Join(t.TempDir(), "ZipTest.zip")
	runCmd(t, "ditto", "-c", "-k", "--keepParent", appPath, zipPath)

	installDir := t.TempDir()
	handled, err := maybeInstallApps(zipPath, mustReadFile(t, zipPath), &Flags{Output: installDir}, io.Discard)
	if err != nil {
		t.Fatalf("maybeInstallApps: %v", err)
	}
	if !handled {
		t.Fatalf("expected app install to be handled")
	}

	dest := filepath.Join(installDir, "ZipTest.app")
	if got := readMarker(t, dest); got != "v1" {
		t.Fatalf("installed marker = %q, want v1", got)
	}
	if hasQuarantine(t, dest) {
		t.Fatalf("expected quarantine attribute to be removed")
	}
}

func TestMaybeInstallAppsTarPreservesSymlink(t *testing.T) {
	srcRoot := t.TempDir()
	appPath := makeTestApp(t, srcRoot, "TarTest", "v1", true, false)
	tarPath := filepath.Join(t.TempDir(), "TarTest.tar.gz")
	runCmd(t, "/usr/bin/tar", "-czf", tarPath, "-C", srcRoot, filepath.Base(appPath))

	installDir := t.TempDir()
	handled, err := maybeInstallApps(tarPath, mustReadFile(t, tarPath), &Flags{Output: installDir}, io.Discard)
	if err != nil {
		t.Fatalf("maybeInstallApps: %v", err)
	}
	if !handled {
		t.Fatalf("expected app install to be handled")
	}

	linkPath := filepath.Join(installDir, "TarTest.app", "Contents", "Frameworks", "Foo.framework", "Versions", "Current")
	fi, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to remain a symlink", linkPath)
	}
}

func TestMaybeInstallAppsDMGInstallsAllAppsByDefault(t *testing.T) {
	srcRoot := t.TempDir()
	makeTestApp(t, srcRoot, "One", "one", false, false)
	makeTestApp(t, srcRoot, "Two", "two", false, false)
	dmgPath := filepath.Join(t.TempDir(), "Apps.dmg")
	runCmd(t, "hdiutil", "create", "-ov", "-fs", "HFS+", "-srcfolder", srcRoot, "-volname", "Apps Volume", "-format", "UDZO", dmgPath)

	installDir := t.TempDir()
	handled, err := maybeInstallApps(dmgPath, mustReadFile(t, dmgPath), &Flags{Output: installDir}, io.Discard)
	if err != nil {
		t.Fatalf("maybeInstallApps: %v", err)
	}
	if !handled {
		t.Fatalf("expected app install to be handled")
	}

	if got := readMarker(t, filepath.Join(installDir, "One.app")); got != "one" {
		t.Fatalf("One.app marker = %q, want one", got)
	}
	if got := readMarker(t, filepath.Join(installDir, "Two.app")); got != "two" {
		t.Fatalf("Two.app marker = %q, want two", got)
	}
}

func TestMaybeInstallAppsFallsBackForZipWithoutApp(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatalf("write tool: %v", err)
	}
	zipPath := filepath.Join(t.TempDir(), "tool.zip")
	runCmd(t, "ditto", "-c", "-k", "--keepParent", bin, zipPath)

	handled, err := maybeInstallApps(zipPath, mustReadFile(t, zipPath), &Flags{Output: t.TempDir()}, io.Discard)
	if err != nil {
		t.Fatalf("maybeInstallApps: %v", err)
	}
	if handled {
		t.Fatalf("expected zip without app bundle to fall back to binary extraction")
	}
}

func TestMaybeInstallAppsConflictActions(t *testing.T) {
	srcRoot := t.TempDir()
	zipV1 := zipTestApp(t, srcRoot, "Conflict", "v1")
	zipV2 := zipTestApp(t, srcRoot, "Conflict", "v2")
	zipV3 := zipTestApp(t, srcRoot, "Conflict", "v3")
	zipV4 := zipTestApp(t, srcRoot, "Conflict", "v4")

	installDir := t.TempDir()
	if handled, err := maybeInstallApps(zipV1, mustReadFile(t, zipV1), &Flags{Output: installDir}, io.Discard); err != nil || !handled {
		t.Fatalf("initial install handled=%v err=%v", handled, err)
	}

	withPromptChoice(t, true, 1, func() {
		if handled, err := maybeInstallApps(zipV2, mustReadFile(t, zipV2), &Flags{Output: installDir}, io.Discard); err != nil || !handled {
			t.Fatalf("replace install handled=%v err=%v", handled, err)
		}
	})
	if got := readMarker(t, filepath.Join(installDir, "Conflict.app")); got != "v2" {
		t.Fatalf("replace marker = %q, want v2", got)
	}

	withPromptChoice(t, true, 2, func() {
		if handled, err := maybeInstallApps(zipV3, mustReadFile(t, zipV3), &Flags{Output: installDir}, io.Discard); err != nil || !handled {
			t.Fatalf("skip install handled=%v err=%v", handled, err)
		}
	})
	if got := readMarker(t, filepath.Join(installDir, "Conflict.app")); got != "v2" {
		t.Fatalf("skip marker = %q, want v2", got)
	}

	withPromptChoice(t, true, 3, func() {
		if _, err := maybeInstallApps(zipV4, mustReadFile(t, zipV4), &Flags{Output: installDir}, io.Discard); err == nil {
			t.Fatalf("expected abort error")
		}
	})
	if got := readMarker(t, filepath.Join(installDir, "Conflict.app")); got != "v2" {
		t.Fatalf("abort marker = %q, want v2", got)
	}

	withPromptChoice(t, false, 0, func() {
		if _, err := maybeInstallApps(zipV4, mustReadFile(t, zipV4), &Flags{Output: installDir}, io.Discard); err == nil {
			t.Fatalf("expected non-interactive conflict error")
		}
	})
}

func TestMaybeInstallAppsRejectsUpgradeOnlyWhenAppsFound(t *testing.T) {
	srcRoot := t.TempDir()
	zipPath := zipTestApp(t, srcRoot, "UpgradeApp", "v1")

	handled, err := maybeInstallApps(zipPath, mustReadFile(t, zipPath), &Flags{
		Output:      t.TempDir(),
		UpgradeOnly: true,
	}, io.Discard)
	if err == nil {
		t.Fatalf("expected --upgrade-only error")
	}
	if handled {
		t.Fatalf("expected install to stop on --upgrade-only")
	}
}

func TestAppInstallTarget(t *testing.T) {
	oldDefault := defaultAppInstallDir
	defaultAppInstallDir = "/Applications"
	t.Cleanup(func() { defaultAppInstallDir = oldDefault })

	dest, err := appInstallTarget("Sample.app", "", 1)
	if err != nil {
		t.Fatalf("default target: %v", err)
	}
	if dest != "/Applications/Sample.app" {
		t.Fatalf("default target = %q", dest)
	}

	dest, err = appInstallTarget("Sample.app", "/tmp/Custom.app", 1)
	if err != nil {
		t.Fatalf("exact target: %v", err)
	}
	if dest != "/tmp/Custom.app" {
		t.Fatalf("exact target = %q", dest)
	}

	if _, err := appInstallTarget("Sample.app", "/tmp/Custom.app", 2); err == nil {
		t.Fatalf("expected exact target to fail for multiple apps")
	}
}

func zipTestApp(t *testing.T, root, name, marker string) string {
	t.Helper()
	appPath := makeTestApp(t, root, name, marker, false, false)
	zipPath := filepath.Join(t.TempDir(), name+".zip")
	runCmd(t, "ditto", "-c", "-k", "--keepParent", appPath, zipPath)
	return zipPath
}

func makeTestApp(t *testing.T, root, name, marker string, withSymlink, withQuarantine bool) string {
	t.Helper()

	appPath := filepath.Join(root, name+".app")
	exeDir := filepath.Join(appPath, "Contents", "MacOS")
	resDir := filepath.Join(appPath, "Contents", "Resources")
	if err := os.MkdirAll(exeDir, 0755); err != nil {
		t.Fatalf("mkdir exe dir: %v", err)
	}
	if err := os.MkdirAll(resDir, 0755); err != nil {
		t.Fatalf("mkdir res dir: %v", err)
	}
	exePath := filepath.Join(exeDir, name)
	if err := os.WriteFile(exePath, []byte("#!/bin/sh\necho test\n"), 0755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	info := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>` + name + `</string>
	<key>CFBundleIdentifier</key>
	<string>dev.eget.` + name + `</string>
	<key>CFBundleName</key>
	<string>` + name + `</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
</dict>
</plist>
`)
	if err := os.WriteFile(filepath.Join(appPath, "Contents", "Info.plist"), info, 0644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resDir, "marker.txt"), []byte(marker), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if withSymlink {
		frameworkDir := filepath.Join(appPath, "Contents", "Frameworks", "Foo.framework", "Versions", "A")
		if err := os.MkdirAll(frameworkDir, 0755); err != nil {
			t.Fatalf("mkdir framework: %v", err)
		}
		if err := os.WriteFile(filepath.Join(frameworkDir, "Foo"), []byte("bin"), 0644); err != nil {
			t.Fatalf("write framework bin: %v", err)
		}
		if err := os.Symlink("A", filepath.Join(appPath, "Contents", "Frameworks", "Foo.framework", "Versions", "Current")); err != nil {
			t.Fatalf("symlink framework: %v", err)
		}
	}

	if withQuarantine {
		runCmd(t, "xattr", "-w", "com.apple.quarantine", "0081;00000000;;", appPath)
	}
	return appPath
}

func readMarker(t *testing.T, appPath string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(appPath, "Contents", "Resources", "marker.txt"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return string(data)
}

func hasQuarantine(t *testing.T, appPath string) bool {
	t.Helper()
	out, err := exec.Command("xattr", appPath).CombinedOutput()
	if err != nil {
		t.Fatalf("xattr list: %v (%s)", err, string(out))
	}
	return bytes.Contains(out, []byte("com.apple.quarantine"))
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func runCmd(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, string(out))
	}
}

func withPromptChoice(t *testing.T, interactive bool, choice int, fn func()) {
	t.Helper()
	oldInteractive := stdinInteractiveFunc
	oldSelect := userSelectFunc
	stdinInteractiveFunc = func() bool { return interactive }
	userSelectFunc = func([]interface{}) int { return choice }
	defer func() {
		stdinInteractiveFunc = oldInteractive
		userSelectFunc = oldSelect
	}()
	fn()
}
