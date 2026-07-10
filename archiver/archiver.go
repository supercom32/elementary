package archiver

/*
Archiver is an interface which defines the standard contract for any sequential file compressor. This allows diverse archive
engines to be plugged into the automated execution runtime transparently.
*/
type Archiver interface {
	AddFile(name string, data []byte) error
	Close() error
}
