package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	pb "github.com/schollz/progressbar/v3"
)

func TestDownloadLocalFileReturnsModTime(t *testing.T) {
	path := t.TempDir() + "/asset.zip"
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	modTime := time.Unix(123, 0)
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes local file: %v", err)
	}

	var buf bytes.Buffer
	meta, err := Download(path, &buf, discardBar)
	if err != nil {
		t.Fatalf("download local file: %v", err)
	}
	if !meta.HasModTime {
		t.Fatalf("expected local file mod time")
	}
	if !meta.ModTime.Equal(modTime) {
		t.Fatalf("mod time = %v, want %v", meta.ModTime, modTime)
	}
	if got := buf.String(); got != "hello" {
		t.Fatalf("buffer = %q, want hello", got)
	}
}

func TestDownloadHTTPReturnsLastModified(t *testing.T) {
	modTime := time.Unix(456, 0).UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", modTime.Format(http.TimeFormat))
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()

	var buf bytes.Buffer
	meta, err := Download(server.URL+"/asset.zip", &buf, discardBar)
	if err != nil {
		t.Fatalf("download http file: %v", err)
	}
	if !meta.HasModTime {
		t.Fatalf("expected HTTP Last-Modified time")
	}
	if !meta.ModTime.Equal(modTime) {
		t.Fatalf("mod time = %v, want %v", meta.ModTime, modTime)
	}
}

func TestDownloadHTTPWithoutLastModifiedLeavesModTimeUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()

	var buf bytes.Buffer
	meta, err := Download(server.URL+"/asset.zip", &buf, discardBar)
	if err != nil {
		t.Fatalf("download http file without last-modified: %v", err)
	}
	if meta.HasModTime {
		t.Fatalf("expected missing mod time when HTTP Last-Modified is absent")
	}
}

func TestSourceTimeForAssetPrefersFinderSourceTime(t *testing.T) {
	finder := &GithubAssetFinder{SourceTime: time.Unix(789, 0)}
	meta := DownloadMetadata{
		ModTime:    time.Unix(123, 0),
		HasModTime: true,
	}

	sourceTime, ok := sourceTimeForAsset(finder, meta)
	if !ok {
		t.Fatalf("expected source time")
	}
	if !sourceTime.Equal(finder.SourceTime) {
		t.Fatalf("source time = %v, want %v", sourceTime, finder.SourceTime)
	}
}

func discardBar(size int64) *pb.ProgressBar {
	return pb.NewOptions64(size, pb.OptionSetWriter(io.Discard))
}
