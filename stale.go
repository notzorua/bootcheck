package main

import (
	"os"
	"path/filepath"
	"strings"
)

// leftover is a directory belonging to a bootloader nobody uses anymore.
type leftover struct {
	path string
	what string
	size uint64
}

// checkLeftovers finds files from other bootloaders on the EFI partition.
//
// Every bootloader keeps its own copy of the kernels. Swap one for another
// and the old copies stay behind, invisible to the other checks: GRUB does
// not reference them, so as far as it is concerned nothing is wrong.
func checkLeftovers(e *env) Check {
	const name = "leftovers"

	if _, err := os.Stat(filepath.Join(e.bootDir, "grub", "grub.cfg")); err != nil {
		return skip(name, "GRUB is not in use here")
	}

	// If the firmware still knows about systemd-boot, it is still wanted.
	if inUse, desc := systemdBootInUEFI(e.varsDir); inUse {
		return ok(name, "UEFI still lists %q, systemd-boot is still needed", desc)
	}

	efiDir := filepath.Join(e.bootDir, "EFI")
	candidates := []struct{ path, what string }{
		{filepath.Join(efiDir, "systemd"), "systemd-boot itself"},
		{filepath.Join(efiDir, "nixos"), "kernels for systemd-boot"},
		{filepath.Join(efiDir, "Linux"), "unified kernel images"},
		{filepath.Join(e.bootDir, "loader"), "systemd-boot configuration"},
	}

	var found []leftover
	var total uint64
	for _, c := range candidates {
		st, err := os.Stat(c.path)
		if err != nil || !st.IsDir() {
			continue
		}
		// Only treat the kernel directory as dead weight when it really
		// holds .efi images, so nothing gets blamed by accident.
		if strings.HasSuffix(c.path, "nixos") && !hasEFIFiles(c.path) {
			continue
		}
		size := dirSize(c.path)
		found = append(found, leftover{path: c.path, what: c.what, size: size})
		total += size
	}

	if len(found) == 0 {
		return ok(name, "no files from other bootloaders")
	}

	c := warn(name, "%s from systemd-boot take up %s",
		plural(len(found), "directory", "directories"), human(total))
	for _, l := range found {
		c = c.detail("%s, %s, %s", l.path, l.what, human(l.size))
	}
	return c.detail("GRUB does not reference these, they can be removed").
		data("bytes", total).
		data("directories", len(found))
}

// systemdBootInUEFI reports whether the firmware still points at systemd-boot.
func systemdBootInUEFI(varsDir string) (bool, string) {
	entries, err := readBootEntries(varsDir)
	if err != nil {
		return false, ""
	}
	for _, entry := range entries {
		d := strings.ToLower(entry.desc)
		if strings.Contains(d, "linux boot manager") || strings.Contains(d, "systemd") {
			return true, entry.desc
		}
	}
	return false, ""
}

// hasEFIFiles reports whether a directory holds any .efi images.
func hasEFIFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".efi") {
			return true
		}
	}
	return false
}

// dirSize adds up every file inside a directory tree.
func dirSize(dir string) uint64 {
	var total uint64
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}
