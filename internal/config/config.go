package config

// Options provides configuration settings for an AtlasDB instance.
type Options struct {
	PageSize        int    // Size of a disk page in bytes (default 4096)
	BufferPoolSize  int    // Maximum number of cached pages in memory (default 256)
	WALSyncOnCommit bool   // If true, call fsync on WAL upon every transaction commit (default true)
	AutoCheckpoint  int    // WAL checkpoint threshold in page updates (default 1000)
}

// DefaultOptions returns standard configuration parameters for AtlasDB.
func DefaultOptions() Options {
	return Options{
		PageSize:        4096,
		BufferPoolSize:  256,
		WALSyncOnCommit: true,
		AutoCheckpoint:  1000,
	}
}
