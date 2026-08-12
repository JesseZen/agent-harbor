package page

import "github.com/asheshgoplani/agent-deck/internal/backend"

// SetBackendForTest swaps Deps.Backend for white-box wiring tests.
func SetBackendForTest(p *Page, b backend.Backend) {
	p.deps.Backend = b
}

// SetDepsLoadNilForTest clears Deps.Load for refresh-reset tests.
func SetDepsLoadNilForTest(p *Page) {
	p.deps.Load = nil
}
