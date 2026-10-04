package colfer

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// File is a single generated output file within a Plan.
type File struct {
	// Path is the slash-separated target location,
	// relative to the commit base directory.
	Path string
	// Content is the generated source code.
	Content []byte

	// replace flags whether the target existed at plan time.
	replace bool
	// digest is the checksum of Content.
	digest [sha256.Size]byte
	// previous is the checksum of the target content at plan
	// time. The value is valid when replace is set only.
	previous [sha256.Size]byte
}

// Digest returns the checksum of the generated Content.
func (f *File) Digest() [sha256.Size]byte { return f.digest }

// Plan is a staged set of generated files. The content is final and
// open for review. Commit applies the Files to a base directory.
type Plan struct {
	// Files are the generated output in order of their Path.
	Files []*File
}

// newFile validates the target location and sets the checksums.
// The path has to be relative and clean, with slashes as the
// separator.
func (p *Plan) newFile(relSlash string, content []byte) (*File, error) {
	if !validPlanPath(relSlash) {
		return nil, fmt.Errorf("colfer: invalid plan path %q", relSlash)
	}
	for _, f := range p.Files {
		if f.Path == relSlash {
			return nil, fmt.Errorf("colfer: duplicate plan path %q", relSlash)
		}
	}

	f := &File{
		Path:    relSlash,
		Content: content,
		digest:  sha256.Sum256(content),
	}
	p.Files = append(p.Files, f)
	return f, nil
}

// validPlanPath returns whether s is a relative, clean, slash-separated
// path within the (commit) base directory.
func validPlanPath(s string) bool {
	if s == "" || s == "." || path.IsAbs(s) || filepath.IsAbs(s) {
		return false
	}
	if path.Clean(s) != s {
		return false
	}
	for _, elem := range strings.Split(s, "/") {
		if elem == ".." || elem == "." || elem == "" {
			return false
		}
	}
	return true
}

// sortFiles orders the plan by target location for deterministic output.
func (p *Plan) sortFiles() {
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
}

// Filesystem operations as variables for fault injection in tests.
var (
	osMkdirAll  = os.MkdirAll
	osMkdirTemp = os.MkdirTemp
	osReadFile  = os.ReadFile
	osWriteFile = os.WriteFile
	osRename    = os.Rename
	osRemove    = os.Remove
	osRemoveAll = os.RemoveAll
)

// Commit writes the planned files under dir. Existing files are replaced
// only when their content still matches the checksum taken at plan time,
// i.e. no external modification happened in the meantime.
//
// The commit stages all content in a temporary directory inside dir
// first, swaps the files one by one, and rolls back to the original
// state on any failure. The procedure is a controlled commit on a single
// machine and directory; it is not an atomic transaction across
// filesystems. Commit never writes paths other than the ones listed in
// the plan.
func (p *Plan) Commit(dir string) error {
	if len(p.Files) == 0 {
		return errors.New("colfer: commit of empty plan")
	}

	// Revalidate the plan: only listed relative paths may be written.
	seen := make(map[string]struct{}, len(p.Files))
	for _, f := range p.Files {
		if !validPlanPath(f.Path) {
			return fmt.Errorf("colfer: commit rejected invalid path %q", f.Path)
		}
		if _, ok := seen[f.Path]; ok {
			return fmt.Errorf("colfer: commit rejected duplicate path %q", f.Path)
		}
		seen[f.Path] = struct{}{}
		if f.digest != sha256.Sum256(f.Content) {
			return fmt.Errorf("colfer: commit rejected tampered content for %q", f.Path)
		}
	}

	// Verify that no external changes occurred since the plan was made.
	for _, f := range p.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		content, err := osReadFile(target)
		switch {
		case os.IsNotExist(err):
			if f.replace {
				return fmt.Errorf("colfer: commit aborted: %q disappeared since plan", f.Path)
			}
		case err != nil:
			return err
		case !f.replace:
			return fmt.Errorf("colfer: commit aborted: %q appeared since plan", f.Path)
		case f.previous != sha256.Sum256(content):
			return fmt.Errorf("colfer: commit aborted: %q changed since plan", f.Path)
		}
	}

	// Stage all content inside the target directory, so that the
	// final moves stay on the same filesystem.
	staging, err := osMkdirTemp(dir, ".colfer-staging-")
	if err != nil {
		return fmt.Errorf("colfer: commit staging: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			osRemoveAll(staging)
		}
	}()

	for _, f := range p.Files {
		staged := filepath.Join(staging, "new", filepath.FromSlash(f.Path))
		if err := osMkdirAll(filepath.Dir(staged), 0o777); err != nil {
			return err
		}
		if err := osWriteFile(staged, f.Content, 0o666); err != nil {
			return err
		}
	}

	// Backup the files to be replaced.
	var backups []*File
	for _, f := range p.Files {
		if !f.replace {
			continue
		}
		backup := filepath.Join(staging, "old", filepath.FromSlash(f.Path))
		if err := osMkdirAll(filepath.Dir(backup), 0o777); err != nil {
			return err
		}
		if err := osRename(filepath.Join(dir, filepath.FromSlash(f.Path)), backup); err != nil {
			return p.rollback(dir, staging, backups, nil, err)
		}
		backups = append(backups, f)
	}

	// Move the staged files into place.
	var moved []*File
	for _, f := range p.Files {
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := osMkdirAll(filepath.Dir(target), 0o777); err != nil {
			return p.rollback(dir, staging, backups, moved, err)
		}
		if err := osRename(filepath.Join(staging, "new", filepath.FromSlash(f.Path)), target); err != nil {
			return p.rollback(dir, staging, backups, moved, err)
		}
		moved = append(moved, f)
	}

	if err := osRemoveAll(staging); err != nil {
		return fmt.Errorf("colfer: commit staging cleanup: %w", err)
	}
	committed = true
	return nil
}

// rollback restores the original files and removes any staged output
// which made it into place already. The failure cause is joined with
// any restoration errors.
func (p *Plan) rollback(dir, staging string, backups, moved []*File, cause error) error {
	errs := []error{cause}

	// Remove the files which were moved into place already.
	for i := len(moved) - 1; i >= 0; i-- {
		target := filepath.Join(dir, filepath.FromSlash(moved[i].Path))
		if err := osRemove(target); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}

	// Restore the backups.
	for i := len(backups) - 1; i >= 0; i-- {
		f := backups[i]
		backup := filepath.Join(staging, "old", filepath.FromSlash(f.Path))
		target := filepath.Join(dir, filepath.FromSlash(f.Path))
		if err := osRename(backup, target); err != nil {
			errs = append(errs, err)
		}
	}

	// Remove directories which were created for the commit. The
	// removal of non-empty directories fails, which protects any
	// pre-existing content.
	for _, f := range p.Files {
		for d := filepath.Dir(filepath.Join(dir, filepath.FromSlash(f.Path))); ; d = filepath.Dir(d) {
			rel, err := filepath.Rel(dir, d)
			if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
			if err := osRemove(d); err != nil {
				break
			}
		}
	}

	return errors.Join(errs...)
}
