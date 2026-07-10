package archiver_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/supercom32/elementary/archiver"
)

/*
readTarEntries is a helper which reads every entry out of a plain, uncompressed TAR file (which, in the
ungraceful-shutdown case, is missing its cosmetic end-of-archive marker) into a name->content map.
*/
func readTarEntries(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("failed to open tar file: %v", err)
	}
	defer file.Close()

	entries := map[string][]byte{}
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			t.Fatalf("failed to read tar entry: %v", err)
		}
		var buf bytes.Buffer
		if _, err := io.Copy(&buf, reader); err != nil {
			t.Fatalf("failed to read tar entry contents: %v", err)
		}
		entries[header.Name] = buf.Bytes()
	}
	return entries
}

/*
TestTarArchiverSurvivesUngracefulShutdown is a test which verifies that every screenshot appended so far remains
fully readable directly from the working ".tar" file even when Close() is never called. This is the core guarantee
this archiver exists to provide.

Example:
    Expected Inputs:
        Two AddFile calls with no Close() call in between or after.

    Expected Outputs:
        The plain ".tar" file on disk is valid and contains both entries after each AddFile call.
*/
func TestTarArchiverSurvivesUngracefulShutdown(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "archiver_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, "test.tar")
	tarEngine, err := archiver.NewTarArchiver(tarPath)
	if err != nil {
		t.Fatalf("failed to create tar archiver: %v", err)
	}

	payloads := map[string][]byte{
		"shot_0.png": []byte("fake image bytes 0"),
		"shot_1.png": []byte("fake image bytes 1"),
	}

	i := 0
	for name, data := range payloads {
		if err := tarEngine.AddFile(name, data); err != nil {
			t.Fatalf("failed to add file: %v", err)
		}
		i++
		// Deliberately never call tarEngine.Close(), simulating a crash mid-run. The plain .tar file
		// must already contain every entry added so far.
		entries := readTarEntries(t, tarPath)
		if len(entries) != i {
			t.Fatalf("after add #%d: expected %d entries in tar, got %d", i, i, len(entries))
		}
	}

	entries := readTarEntries(t, tarPath)
	for name, expected := range payloads {
		actual, ok := entries[name]
		if !ok {
			t.Errorf("expected entry %s in tar, not found", name)
			continue
		}
		if !bytes.Equal(actual, expected) {
			t.Errorf("expected content %q for %s, got %q", expected, name, actual)
		}
	}

	// Close was never called: no .tar.gz should exist, and the plain .tar must remain in place.
	if _, err := os.Stat(tarPath + ".gz"); !os.IsNotExist(err) {
		t.Errorf("expected no .tar.gz to exist without a graceful Close(), stat err: %v", err)
	}
}

/*
TestTarArchiverGracefulClose is a test which verifies that a graceful Close() gzip-compresses the finished archive
into "<path>.gz" and removes the intermediate plain ".tar" file.

Example:
    Expected Inputs:
        A call to NewTarArchiver and AddFile with a test payload, followed by Close().

    Expected Outputs:
        A valid "<path>.gz" file exists containing the archived payload, and the plain ".tar" file is gone.
*/
func TestTarArchiverGracefulClose(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "archiver_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, "test.tar")
	tarEngine, err := archiver.NewTarArchiver(tarPath)
	if err != nil {
		t.Fatalf("failed to create tar archiver: %v", err)
	}

	payload := []byte("hello elementary archiver")
	if err := tarEngine.AddFile("test.txt", payload); err != nil {
		t.Fatalf("failed to add file: %v", err)
	}
	if err := tarEngine.Close(); err != nil {
		t.Fatalf("failed to close archiver: %v", err)
	}

	if _, err := os.Stat(tarPath); !os.IsNotExist(err) {
		t.Errorf("expected intermediate .tar file to be removed after Close(), stat err: %v", err)
	}

	gzipPath := tarPath + ".gz"
	file, err := os.Open(gzipPath)
	if err != nil {
		t.Fatalf("failed to open generated tar.gz: %v", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("failed to open gzip stream: %v", err)
	}
	defer gzipReader.Close()

	reader := tar.NewReader(gzipReader)
	header, err := reader.Next()
	if err != nil {
		t.Fatalf("failed to read tar entry: %v", err)
	}
	if header.Name != "test.txt" {
		t.Errorf("expected file name test.txt, got %s", header.Name)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		t.Fatalf("failed to read file contents inside tar: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("expected content %s, got %s", string(payload), buf.String())
	}

	if _, err := reader.Next(); err != io.EOF {
		t.Errorf("expected exactly 1 entry in tar, found more")
	}
}
