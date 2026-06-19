package mcpclient

import (
	"context"
	"net/http"
	"strconv"
)

const (
	RecursionDepthHeader = "X-Ragent-MCP-Depth"
	maxRecursionDepth    = 1
)

type recursionDepthKey struct{}

func WithRecursionDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, recursionDepthKey{}, depth)
}

func RecursionDepth(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	depth, _ := ctx.Value(recursionDepthKey{}).(int)
	return depth
}

func InboundDepthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		depth, _ := strconv.Atoi(r.Header.Get(RecursionDepthHeader))
		next.ServeHTTP(w, r.WithContext(WithRecursionDepth(r.Context(), depth)))
	})
}
