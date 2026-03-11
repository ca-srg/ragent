package evalexport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Writer struct {
	dirPath string
	mu      sync.Mutex
}

func NewWriter(dirPath string) (*Writer, error) {
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dirPath, err)
	}

	return &Writer{dirPath: dirPath}, nil
}

func (w *Writer) WriteRecord(record *EvalRecord) (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	filename := fmt.Sprintf("eval_%s.jsonl", time.Now().UTC().Format("2006-01-02"))
	filePath := filepath.Join(w.dirPath, filename)

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close file %s: %w", filePath, cerr)
		}
	}()

	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal record: %w", err)
	}

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write record: %w", err)
	}

	return nil
}
