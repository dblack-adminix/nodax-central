//go:build !windows

package main

func isWindowsService() (bool, error) {
	return false, nil
}

func runWindowsService(name string, run func(<-chan struct{}) error) error {
	return nil
}
