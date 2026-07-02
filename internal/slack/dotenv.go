package slack

import (
	"bufio"
	"os"
	"strings"
)

// parseDotEnv reads a minimal .env file: KEY=VALUE lines, with # comments,
// blank lines, optional surrounding quotes, and an optional leading
// "export ". Keys are kept verbatim (so hyphenated keys like
// SLACK-WEBHOOK-TOKEN are preserved). Returns the raw error from os.Open on
// failure so the caller can distinguish "not present" via os.IsNotExist.
func parseDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if key != "" {
			out[key] = val
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
