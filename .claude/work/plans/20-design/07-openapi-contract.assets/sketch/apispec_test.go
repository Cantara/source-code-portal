package apispec_test

import (
	"bytes"
	"testing"

	"example.com/scp/internal/apispec"
)

func TestEmbedded(t *testing.T) {
	if !bytes.HasPrefix(apispec.Public(), []byte("openapi: 3.1.0")) || !bytes.HasPrefix(apispec.Admin(), []byte("openapi: 3.1.0")) {
		t.Fatal("bundles are not OpenAPI 3.1 documents")
	}
	if !bytes.Contains(apispec.Admin(), []byte("operationId: retryJob")) || bytes.Contains(apispec.Public(), []byte("operationId: retryJob")) {
		t.Fatal("admin must be a superset, public must not carry admin operations")
	}
}
