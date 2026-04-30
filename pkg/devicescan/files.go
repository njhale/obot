package devicescan

import (
	"errors"
	"io/fs"
	"path"
	"unicode/utf8"

	"github.com/obot-platform/obot/apiclient/types"
)

// File-collection limits, mirroring runlayer's file_collector defaults.
const (
	maxFileBytes     int64 = 1 << 20 // 1 MiB — files above this are recorded with Oversized=true, no Content
	maxArtifactBytes int64 = 5 << 20 // 5 MiB — running budget per skill/plugin
)

// artifactSkipDirs are dependency / build directories we never descend into
// when walking inside a skill or plugin directory.
var artifactSkipDirs = map[string]bool{
	"node_modules": true,
	".venv":        true,
	"venv":         true,
	"vendor":       true,
	"dist":         true,
	".tox":         true,
	".git":         true,
	"__pycache__":  true,
}

// AddFile reads and records the file at rel (relative to r.fsys). Returns
// the absolute path observations should reference. Idempotent: a repeated
// call for the same rel is a cheap map lookup that returns the
// previously-stored entry.
//
// Files larger than maxFileBytes are recorded with Oversized=true and no
// Content (SizeBytes is still populated when Stat succeeds).
func (r *Result) AddFile(rel string) (string, error) {
	abs := r.abs(rel)
	if _, ok := r.files[abs]; ok {
		return abs, nil
	}

	info, err := fs.Stat(r.fsys, rel)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", errors.New("AddFile: path is a directory")
	}

	f := types.DeviceScanFile{
		Path:      abs,
		SizeBytes: info.Size(),
	}

	if info.Size() > maxFileBytes {
		f.Oversized = true
		r.files[abs] = f
		return abs, nil
	}

	data, err := fs.ReadFile(r.fsys, rel)
	if err != nil {
		// Treat as oversized/unreadable so the observation still references it.
		f.Oversized = true
		r.files[abs] = f
		return abs, nil
	}

	if utf8.Valid(data) {
		f.Content = string(data)
	}
	r.files[abs] = f
	return abs, nil
}

// addOversizedPlaceholder records a file at rel with Oversized=true and no
// Content, used when an artifact-level budget has been exhausted.
func (r *Result) addOversizedPlaceholder(rel string, size int64) string {
	abs := r.abs(rel)
	if existing, ok := r.files[abs]; ok {
		return existing.Path
	}
	r.files[abs] = types.DeviceScanFile{
		Path:      abs,
		SizeBytes: size,
		Oversized: true,
	}
	return abs
}

// collectArtifactFiles walks the directory rooted at dirRel, collecting
// files whose extension is in allowedExts. Dependency directories are
// skipped. Files past the per-artifact byte budget are recorded as
// Oversized placeholders with no Content; the returned oversized flag is
// true if any file was over-sized or the budget was exhausted.
func (r *Result) collectArtifactFiles(dirRel string, allowedExts map[string]bool) (paths []string, oversized bool) {
	var total int64
	_ = fs.WalkDir(r.fsys, dirRel, func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if rel != dirRel && artifactSkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		ext := path.Ext(rel)
		if !allowedExts[ext] {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		size := info.Size()

		// Per-file oversize cap.
		if size > maxFileBytes {
			oversized = true
			abs := r.addOversizedPlaceholder(rel, size)
			paths = append(paths, abs)
			return nil
		}
		// Per-artifact budget.
		if total+size > maxArtifactBytes {
			oversized = true
			abs := r.addOversizedPlaceholder(rel, size)
			paths = append(paths, abs)
			return nil
		}
		total += size

		abs, addErr := r.AddFile(rel)
		if addErr != nil {
			return nil
		}
		paths = append(paths, abs)
		return nil
	})
	return paths, oversized
}
