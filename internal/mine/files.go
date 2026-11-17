package mine

import "os"

func readFileString(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func writeFileString(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
