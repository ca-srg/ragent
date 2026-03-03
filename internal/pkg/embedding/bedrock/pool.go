package bedrock

import (
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
)

var (
	sharedClientsMu sync.Mutex
	sharedClients   = make(map[string]*BedrockClient)
)

func clientPoolKey(cfg aws.Config, modelID string) string {
	credPtr := ""
	if cfg.Credentials != nil {
		credPtr = fmt.Sprintf("%p", cfg.Credentials)
	}
	return fmt.Sprintf("%s|%s|%s", cfg.Region, modelID, credPtr)
}

// GetSharedBedrockClient returns a process-wide cached Bedrock client for the given region/model.
// Reusing the client ensures HTTP/2 connections are pooled instead of recreated on each call.
func GetSharedBedrockClient(cfg aws.Config, modelID string) *BedrockClient {
	key := clientPoolKey(cfg, modelID)

	sharedClientsMu.Lock()
	defer sharedClientsMu.Unlock()

	if client, ok := sharedClients[key]; ok {
		return client
	}

	client := NewBedrockClient(cfg, modelID)
	sharedClients[key] = client
	return client
}
