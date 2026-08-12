//go:build unix

package coreclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
)

func newUnixHTTPClient(socketPath string) (*http.Client, func(), string, error) {
	if socketPath == "" || !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath {
		return nil, nil, "", fmt.Errorf("%w: path must be absolute and clean", ErrInvalidSocket)
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: %v", ErrInvalidSocket, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeSocket == 0 {
		return nil, nil, "", fmt.Errorf("%w: path is not a direct Unix socket", ErrInvalidSocket)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil, nil, "", fmt.Errorf("%w: socket owner is unavailable", ErrWrongSocketOwner)
	}
	if int(stat.Uid) != os.Geteuid() {
		return nil, nil, "", fmt.Errorf("%w: uid %d", ErrWrongSocketOwner, stat.Uid)
	}

	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		ForceAttemptHTTP2: false,
	}
	client := &http.Client{Transport: transport}
	return client, transport.CloseIdleConnections, socketPath, nil
}
