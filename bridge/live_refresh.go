package main

func isRuntimeRefresh(msg any) bool {
	switch msg.(type) {
	case appLoaded, appRefreshTick:
		return true
	}
	return false
}
