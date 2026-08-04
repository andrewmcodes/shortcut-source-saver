package archive

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"howett.net/plist"
)

const (
	ModeAnyone          = "anyone"
	ModePeopleWhoKnowMe = "people-who-know-me"
)

func NormalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ModePeopleWhoKnowMe:
		return ModePeopleWhoKnowMe
	default:
		return ModeAnyone
	}
}

func Sign(jsonChanged bool, wf map[string]any, outPath, mode string) error {
	if signedOutputIsStillValid(jsonChanged, outPath) {
		return nil
	}

	data, err := plist.Marshal(wf, plist.BinaryFormat)
	if err != nil {
		return fmt.Errorf("encoding binary plist for %s: %w", outPath, err)
	}

	tmp, err := os.CreateTemp("", "sss-unsigned-*.shortcut")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	cmd := exec.Command("shortcuts", "sign", "--mode", NormalizeMode(mode), "-i", tmp.Name(), "-o", outPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("shortcuts sign %s: %w: %s", outPath, err, msg)
		}
		return fmt.Errorf("shortcuts sign %s: %w", outPath, err)
	}
	return nil
}

func signedOutputIsStillValid(jsonChanged bool, outPath string) bool {
	if jsonChanged {
		return false
	}
	_, err := os.Stat(outPath)
	return err == nil
}
