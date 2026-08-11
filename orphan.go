package main

import (
	"os"
	"path/filepath"
	"sort"
)

// checkOrphans finds kernels sitting on the partition that no menu entry
// points at.
//
// This is the mirror image of the kernels check. That one asks whether every
// referenced file exists; this one asks whether every existing file is
// referenced. Files that are neither are dead weight, usually left by
// generations that were garbage collected while their kernels stayed put.
func checkOrphans(e *env) Check {
	const name = "orphans"

	if e.refsErr != nil {
		return skip(name, "the GRUB config could not be read")
	}

	kernelsDir := filepath.Join(e.bootDir, "kernels")
	entries, err := os.ReadDir(kernelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return skip(name, "%s does not exist", kernelsDir)
		}
		return skip(name, "cannot read %s: %v", kernelsDir, err)
	}

	referenced := map[string]bool{}
	for _, r := range e.refs {
		referenced[filepath.Base(r.path)] = true
	}

	var orphans []string
	var total uint64
	for _, entry := range entries {
		if entry.IsDir() || referenced[entry.Name()] {
			continue
		}
		orphans = append(orphans, entry.Name())
		if info, err := entry.Info(); err == nil {
			total += uint64(info.Size())
		}
	}

	if len(orphans) == 0 {
		return ok(name, "every file in %s is referenced by the menu", kernelsDir)
	}

	sort.Strings(orphans)
	c := warn(name, "%s in %s are not referenced by any menu entry, %s",
		plural(len(orphans), "file", "files"), kernelsDir, human(total))
	for _, o := range orphans {
		c = c.detail("%s", o)
	}
	return c.detail("left over from generations that no longer exist").
		detail("a rebuild normally clears these, `nixos-rebuild boot` will retry").
		data("files", len(orphans)).
		data("bytes", total)
}
