package domain

import "context"

type FileScanner interface {
	ScanDirectory(dirPath string) ([]*FileInfo, error)
}

type Vectorizer interface {
	VectorizeFiles(ctx context.Context, files []*FileInfo, dryRun bool) (*ProcessingResult, error)
}
