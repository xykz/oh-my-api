//go:build !windows

package proxy

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
