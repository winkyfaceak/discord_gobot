package commands

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
)

// saveJSON writes v to path atomically (temp file, then rename), so a crash
// mid-write can't corrupt saved data.
func saveJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// loadJSON reads path into v. A missing file is not an error and leaves v as is.
func loadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
