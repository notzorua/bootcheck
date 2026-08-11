package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// checkSpace asks whether the EFI partition has room for a few more
// generations. When it runs out, a new kernel is written only halfway
// and the bootloader breaks.
func checkSpace(e *env) Check {
	const name = "space"

	var st syscall.Statfs_t
	if err := syscall.Statfs(e.bootDir, &st); err != nil {
		return fail(name, "cannot stat %s: %v", e.bootDir, err)
	}

	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	free := st.Bavail * bs

	withNumbers := func(c Check) Check {
		return c.data("free_bytes", free).data("total_bytes", total)
	}

	pair := generationSize(e.bootDir)
	if pair == 0 {
		return withNumbers(ok(name, "%s free of %s", human(free), human(total)))
	}

	fits := int(free / pair)
	detail := func(c Check) Check {
		return withNumbers(c).
			detail("one generation takes about %s", human(pair)).
			data("generations_left", fits)
	}

	switch {
	case fits < 2:
		return detail(fail(name, "only %s free, less than two generations of %s",
			human(free), human(pair))).
			detail("the next rebuild may not fit")
	case fits < 5:
		return detail(warn(name, "%s free, room for about %s",
			human(free), plural(fits, "generation", "generations"))).
			detail("lower configurationLimit or remove old generations")
	default:
		return detail(ok(name, "%s free of %s, room for about %s",
			human(free), human(total), plural(fits, "generation", "generations")))
	}
}

// generationSize estimates what one generation costs on the partition:
// the largest kernel plus the largest initrd.
func generationSize(bootDir string) uint64 {
	entries, err := os.ReadDir(filepath.Join(bootDir, "kernels"))
	if err != nil {
		return 0
	}
	var maxKernel, maxInitrd uint64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		size := uint64(info.Size())
		switch {
		case strings.HasSuffix(entry.Name(), "-bzImage"):
			if size > maxKernel {
				maxKernel = size
			}
		case strings.HasSuffix(entry.Name(), "-initrd"):
			if size > maxInitrd {
				maxInitrd = size
			}
		}
	}
	return maxKernel + maxInitrd
}

// checkIntegrity looks for signs that the partition was repaired, and for
// files left behind by bootloaders that are no longer in use.
func checkIntegrity(e *env) Check {
	const name = "integrity"

	// Files like FSCK0000.REC are written by the FAT repair tool, which
	// parks orphaned clusters there. Their presence means the partition
	// was damaged at some point.
	recs, _ := filepath.Glob(filepath.Join(e.bootDir, "*.REC"))
	sort.Strings(recs)

	// A loader directory is left over from systemd-boot. If grub sits next
	// to it, the bootloader was swapped and the old one was never removed.
	loaderDir := filepath.Join(e.bootDir, "loader")
	_, loaderErr := os.Stat(loaderDir)
	_, grubErr := os.Stat(filepath.Join(e.bootDir, "grub"))
	staleLoader := loaderErr == nil && grubErr == nil

	if len(recs) == 0 && !staleLoader {
		return ok(name, "no FAT repair leftovers, no stale bootloader config")
	}

	c := warn(name, "the partition carries traces of past trouble")
	if len(recs) > 0 {
		c = c.detail("%s left by a FAT repair:", plural(len(recs), "file", "files"))
		for _, r := range recs {
			c = c.detail("  %s", r)
		}
		c = c.detail("the partition was repaired once, usually after a hard power off")
		c = c.detail("the files hold orphaned data and can be deleted")
		c = c.data("fsck_files", len(recs))
	}
	if staleLoader {
		c = c.detail("%s is left over from systemd-boot, harmless but unused", loaderDir)
		c = c.data("stale_loader_dir", true)
	}
	return c
}
