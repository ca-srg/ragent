package vectorizer

import pkgdomain "github.com/ca-srg/ragent/internal/pkg/domain"

func applyUploadSecretMetadata(metadata *pkgdomain.DocumentMetadata, secret bool) {
	if metadata == nil {
		return
	}

	metadata.Secret = secret
	if metadata.CustomFields == nil {
		metadata.CustomFields = map[string]interface{}{}
	}
	metadata.CustomFields["secret"] = secret
}
