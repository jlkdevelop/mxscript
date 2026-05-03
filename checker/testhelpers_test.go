package checker

import "os"

// readFileForTest is a thin wrapper around os.ReadFile, kept in a
// separate helpers file so it doesn't pollute the main test file's
// import list.
func readFileForTest(path string) ([]byte, error) {
	return os.ReadFile(path)
}
