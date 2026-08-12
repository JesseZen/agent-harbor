package coreclient

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const adminOpenAPISHA256 = "7ee8b925c3de5bf2f53bcf63c4cb32a7388a90692f4b1b08c543132e70ff8291"

func TestAdminOpenAPISourceHash(t *testing.T) {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve schema test source path")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "api", "v3", "admin.openapi.yaml")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read public Admin OpenAPI source: %v", err)
	}

	sum := sha256.Sum256(contents)
	if got := hex.EncodeToString(sum[:]); got != adminOpenAPISHA256 {
		t.Fatalf("Admin OpenAPI SHA-256 = %s, want %s", got, adminOpenAPISHA256)
	}
}
