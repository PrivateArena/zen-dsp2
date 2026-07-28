package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jang/zen-dsp2/pwfilter"
)

type CurveData struct {
	Gains [pwfilter.NumBands]float64 `json:"gains"`
}

func Path() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	p := filepath.Join(dir, "eqd")
	if err := os.MkdirAll(p, 0755); err != nil {
		return "", err
	}
	return filepath.Join(p, "curve.json"), nil
}

func Load() (*CurveData, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var cd CurveData
	if err := json.Unmarshal(b, &cd); err != nil {
		return nil, err
	}
	return &cd, nil
}

func Save(gains [pwfilter.NumBands]float64) error {
	p, err := Path()
	if err != nil {
		return err
	}
	cd := CurveData{Gains: gains}
	b, err := json.MarshalIndent(cd, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
