package archiver

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/supercom32/filesystem"
)

/*
TarArchiver is a structure which orchestrates writing sequential files into a TAR archive, appending each new file
directly to the end of the working archive as it arrives. In addition, the following should be noted:

  - Unlike ZIP, TAR has no central directory/index that must be finalized for prior entries to be readable: it is
    just a flat sequence of [header][padded data] records. This means every entry is fully valid and extractable
    the moment AddFile returns, independent of whatever happens afterward. An ungracefully terminated process
    (crash, panic, kill -9) leaves every screenshot captured up to that point intact and readable with any
    standard tar tool, at the cost of the working file being plain, uncompressed ".tar" rather than compressed.

  - Close() is only responsible for the graceful-shutdown path: it writes the standard tar end-of-archive marker,
    then gzip-compresses the finished archive into "<filePath>.gz" and removes the intermediate plain ".tar" file.
    If Close() is never reached, the plain ".tar" file (missing only the cosmetic end-of-archive marker, which
    standard tar readers tolerate) is what is left behind.
*/
type TarArchiver struct {
	filePath string
}

// tarEndOfArchiveSize is the size, in bytes, of the two zero-filled 512-byte records that mark the end of a tar
// archive per the POSIX/GNU tar format. It is written only on a graceful Close(), never during AddFile.
const tarEndOfArchiveSize = 1024

var tarNameReplacer = strings.NewReplacer(
	"/", "_", "\\", "_", "?", "_", "%", "_", "*", "_",
	":", "_", "|", "_", "\"", "_", "<", "_", ">", "_",
)

/*
NewTarArchiver is a function which instantiates and configures a new TarArchiver instance, creating the target
directory if necessary.
*/
func NewTarArchiver(filePath string) (*TarArchiver, error) {
	if filePath == "" {
		return nil, fmt.Errorf("archive file path cannot be empty")
	}
	dir := filepath.Dir(filePath)
	normalizedDir := filesystem.GetNormalizedDirectoryPath(dir)
	if err := filesystem.CreateDirectory(normalizedDir, 0); err != nil {
		return nil, fmt.Errorf("failed to create archive directory: %w", err)
	}
	return &TarArchiver{filePath: filePath}, nil
}

/*
AddFile is a method which appends a single named entry to the end of the working TAR file and fsyncs it before
returning. Every previously appended entry is left completely untouched, since this call only ever writes the new
header and data, so cost and memory use are proportional to the new file alone, never to how many entries (or how
much data) were archived before it.
*/
func (shared *TarArchiver) AddFile(name string, data []byte) error {
	sanitizedName := tarNameReplacer.Replace(name)

	file, err := os.OpenFile(shared.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := tar.NewWriter(file)
	header := &tar.Header{
		Name:    sanitizedName,
		Mode:    0644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		return err
	}
	// Flush pads the entry to the required 512-byte boundary without writing the end-of-archive marker that
	// Writer.Close would add. That marker is deferred to our own Close(), on the graceful-shutdown path only.
	if err := writer.Flush(); err != nil {
		return err
	}
	return file.Sync()
}

/*
Close is a method which finalizes the archive for the graceful-shutdown path only: it appends the standard
end-of-archive marker to the working TAR file, gzip-compresses the result into "<filePath>.gz" (written to a
temporary file and renamed into place atomically), and removes the intermediate plain TAR file. If AddFile was never
called, there is no archive to finalize and this is a no-op.
*/
func (shared *TarArchiver) Close() error {
	if _, err := os.Stat(shared.filePath); os.IsNotExist(err) {
		return nil
	}

	file, err := os.OpenFile(shared.filePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	if _, err := file.Write(make([]byte, tarEndOfArchiveSize)); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	if err := shared.compress(); err != nil {
		return err
	}
	return os.Remove(shared.filePath)
}

/*
compress is a method which gzips the finalized TAR file into "<filePath>.gz" via a temporary file plus atomic
rename, so a failure or crash mid-compression leaves the still-valid plain TAR file untouched rather than a
half-written gzip.
*/
func (shared *TarArchiver) compress() error {
	sourceFile, err := os.Open(shared.filePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	gzipPath := shared.filePath + ".gz"
	tempFilePath := gzipPath + ".tmp"
	destinationFile, err := os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	gzipWriter := gzip.NewWriter(destinationFile)
	if _, err := io.Copy(gzipWriter, sourceFile); err != nil {
		_ = gzipWriter.Close()
		_ = destinationFile.Close()
		_ = os.Remove(tempFilePath)
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		_ = destinationFile.Close()
		_ = os.Remove(tempFilePath)
		return err
	}
	if err := destinationFile.Close(); err != nil {
		_ = os.Remove(tempFilePath)
		return err
	}
	return os.Rename(tempFilePath, gzipPath)
}
