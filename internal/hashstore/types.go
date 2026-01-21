package hashstore

import "time"

// ChangeType represents the type of change detected for a file
type ChangeType int

const (
	// ChangeTypeNone indicates no change detected
	ChangeTypeNone ChangeType = iota
	// ChangeTypeNew indicates a new file
	ChangeTypeNew
	// ChangeTypeModified indicates a modified file
	ChangeTypeModified
	// ChangeTypeDeleted indicates a deleted file
	ChangeTypeDeleted
)

// String returns the string representation of ChangeType
func (c ChangeType) String() string {
	switch c {
	case ChangeTypeNone:
		return "none"
	case ChangeTypeNew:
		return "new"
	case ChangeTypeModified:
		return "modified"
	case ChangeTypeDeleted:
		return "deleted"
	default:
		return "unknown"
	}
}

// FileHashRecord represents a stored file hash record
type FileHashRecord struct {
	ID           int64
	SourceType   string // "local" or "s3"
	FilePath     string
	ContentHash  string // MD5 hash in hex format
	FileSize     int64
	VectorizedAt time.Time
}

// FileChange represents a detected change for a file
type FileChange struct {
	FilePath   string
	SourceType string
	ChangeType ChangeType
	OldHash    string // Existing hash (for modified/deleted)
	NewHash    string // New hash (for new/modified)
}

// ChangeDetectionResult holds the results of change detection
type ChangeDetectionResult struct {
	ToProcess     []FileChange // Files to be processed (new or modified)
	Unchanged     []string     // Files with no changes (skipped)
	Deleted       []string     // Files that were deleted
	NewCount      int
	ModCount      int
	UnchangeCount int
	DeleteCount   int
}
