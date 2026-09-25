// Package site — ownership.go tracks which generated files srv still owns so
// force regeneration can never silently revert hand edits. Files the generator
// labels "yours to edit" (nginx.conf, docker-compose.yml) are written with a
// recorded content hash; a force regen replaces a file in place only while its
// bytes still match that record, and otherwise leaves it untouched, writing
// the fresh render beside it as "<name>.regenerated" for the user to merge.
package site

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/fsutil"
)

// generatedHashesFile is the hidden sidecar in a site's config dir that maps
// file basename -> sha256 of the content srv last wrote there.
const generatedHashesFile = ".generated-hashes"

// readGeneratedHashes loads the ownership record for a site dir. A missing or
// unreadable record yields an empty map: files with no recorded hash are
// treated as not owned, so an unknown-provenance file is preserved rather
// than clobbered.
func readGeneratedHashes(siteDir string) map[string]string {
	data, err := os.ReadFile(filepath.Join(siteDir, generatedHashesFile))
	if err != nil {
		return map[string]string{}
	}
	var hashes map[string]string
	if err := yaml.Unmarshal(data, &hashes); err != nil {
		return map[string]string{}
	}
	if hashes == nil {
		hashes = map[string]string{}
	}
	return hashes
}

// saveGeneratedHashes persists the ownership record. Best-effort: a failure
// only costs one extra ".regenerated" round on the next reload.
func saveGeneratedHashes(siteDir string, hashes map[string]string) {
	keys := make([]string, 0, len(hashes))
	for k := range hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(hashes))
	for _, k := range keys {
		out[k] = hashes[k]
	}
	data, err := yaml.Marshal(out)
	if err != nil {
		return
	}
	_ = fsutil.AtomicWriteFile(filepath.Join(siteDir, generatedHashesFile), data, constants.FilePermDefault)
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// writeOwnedFile writes content to path during a (possibly forced)
// regeneration. Behaviour by case:
//
//   - absent file: write it and record the hash.
//   - present, force=false: leave untouched (the caller may have customized).
//   - present, force=true, still owned (bytes match the recorded hash, or are
//     byte-identical to the fresh render): replace in place.
//   - present, force=true, diverged: preserve the file and write the fresh
//     render next to it as "<name>.regenerated", returning true so the caller
//     can warn.
//
// recordedHashes is updated in place for every content srv is responsible for;
// the caller persists it with saveGeneratedHashes once all writes succeed.
func writeOwnedFile(path string, content []byte, force bool, recordedHashes map[string]string) (regenerated bool, err error) {
	base := filepath.Base(path)
	existing, readErr := os.ReadFile(path)

	if readErr != nil {
		if err := fsutil.AtomicWriteFile(path, content, constants.FilePermDefault); err != nil {
			return false, err
		}
		recordedHashes[base] = contentHash(content)
		return false, nil
	}

	if string(existing) == string(content) {
		recordedHashes[base] = contentHash(content)
		return false, nil
	}

	if !force {
		return false, nil
	}

	if contentHash(existing) != recordedHashes[base] {
		regenPath := path + ".regenerated"
		if err := fsutil.AtomicWriteFile(regenPath, content, constants.FilePermDefault); err != nil {
			return false, fmt.Errorf("write %s: %w", filepath.Base(regenPath), err)
		}
		// Record the fresh content so adopting it (copying the
		// .regenerated file over the original) hands ownership back.
		recordedHashes[base] = contentHash(content)
		return true, nil
	}

	if err := fsutil.AtomicWriteFile(path, content, constants.FilePermDefault); err != nil {
		return false, err
	}
	recordedHashes[base] = contentHash(content)
	return false, nil
}
