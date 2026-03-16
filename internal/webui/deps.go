package webui

import domain "github.com/ca-srg/ragent/internal/pkg/domain"

type Dependencies struct {
	FileScanner domain.FileScanner
	Vectorizer  domain.Vectorizer
}
