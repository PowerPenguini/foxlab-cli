package lab

import "os"

func regularFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return path, true
}

func existingNonRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.Mode().IsRegular()
}
