//go:build !unix

package coreclient

import (
	"fmt"
	"net/http"
)

func newUnixHTTPClient(string) (*http.Client, func(), string, error) {
	return nil, nil, "", fmt.Errorf("%w: Unix sockets are not supported on this platform", ErrInvalidSocket)
}
