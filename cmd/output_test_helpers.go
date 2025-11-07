package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func captureOutput(t testing.TB, fn func()) string {
	t.Helper()
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	defer func() {
		_ = readPipe.Close()
	}()

	originalStdout := os.Stdout
	os.Stdout = writePipe
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := writePipe.Close(); err != nil {
		t.Fatalf("failed to close write pipe: %v", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, readPipe); err != nil {
		t.Fatalf("failed to read captured output: %v", err)
	}

	return buf.String()
}
