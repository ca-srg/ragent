package mcpclient

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInboundDepthMiddlewareClampsNegativeDepthToZero(t *testing.T) {
	var gotDepth int
	handler := InboundDepthMiddleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		gotDepth = RecursionDepth(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RecursionDepthHeader, "-999")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Equal(t, 0, gotDepth)
}
