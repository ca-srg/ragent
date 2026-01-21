package hashstore

import (
	"context"

	"github.com/ca-srg/ragent/internal/types"
)

// ChangeDetector detects file changes by comparing current files with stored hashes
type ChangeDetector struct {
	store *HashStore
}

// NewChangeDetector creates a new ChangeDetector
func NewChangeDetector(store *HashStore) *ChangeDetector {
	return &ChangeDetector{store: store}
}

// DetectChanges compares current files with stored hashes and returns changes.
// sourceTypes can be a single type like "local" or "s3", or multiple types for mixed mode.
func (d *ChangeDetector) DetectChanges(
	ctx context.Context,
	sourceTypes []string,
	files []*types.FileInfo,
) (*ChangeDetectionResult, error) {
	// Get all existing hashes for the given source types
	existingHashes, err := d.store.GetAllFileHashesForSourceTypes(ctx, sourceTypes)
	if err != nil {
		return nil, err
	}

	result := &ChangeDetectionResult{
		ToProcess: make([]FileChange, 0),
		Unchanged: make([]string, 0),
		Deleted:   make([]string, 0),
	}

	// Track which existing files we've seen
	seenPaths := make(map[string]bool)

	// Check each current file
	for _, file := range files {
		seenPaths[file.Path] = true

		// Use file's own source type for the change record
		fileSourceType := file.SourceType
		if fileSourceType == "" {
			fileSourceType = "local"
		}

		existingRecord, exists := existingHashes[file.Path]
		if !exists {
			// New file
			result.ToProcess = append(result.ToProcess, FileChange{
				FilePath:   file.Path,
				SourceType: fileSourceType,
				ChangeType: ChangeTypeNew,
				NewHash:    file.ContentHash,
			})
			result.NewCount++
		} else if existingRecord.ContentHash != file.ContentHash {
			// Modified file
			result.ToProcess = append(result.ToProcess, FileChange{
				FilePath:   file.Path,
				SourceType: fileSourceType,
				ChangeType: ChangeTypeModified,
				OldHash:    existingRecord.ContentHash,
				NewHash:    file.ContentHash,
			})
			result.ModCount++
		} else {
			// Unchanged
			result.Unchanged = append(result.Unchanged, file.Path)
			result.UnchangeCount++
		}
	}

	// Find deleted files (in existing hashes but not in current files)
	for filePath := range existingHashes {
		if !seenPaths[filePath] {
			result.Deleted = append(result.Deleted, filePath)
			result.DeleteCount++
		}
	}

	return result, nil
}

// FilterFilesToProcess returns only the files that need to be processed.
// sourceTypes can be a single type like []string{"local"} or multiple types for mixed mode.
func (d *ChangeDetector) FilterFilesToProcess(
	ctx context.Context,
	sourceTypes []string,
	files []*types.FileInfo,
) ([]*types.FileInfo, *ChangeDetectionResult, error) {
	changes, err := d.DetectChanges(ctx, sourceTypes, files)
	if err != nil {
		return nil, nil, err
	}

	// Build a set of files that need processing
	toProcessPaths := make(map[string]bool)
	for _, change := range changes.ToProcess {
		toProcessPaths[change.FilePath] = true
	}

	// Filter the original files list
	filtered := make([]*types.FileInfo, 0, len(changes.ToProcess))
	for _, file := range files {
		if toProcessPaths[file.Path] {
			filtered = append(filtered, file)
		}
	}

	return filtered, changes, nil
}
