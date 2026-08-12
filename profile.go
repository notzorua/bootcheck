package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultProfilesDir = "/nix/var/nix/profiles"

// systemProfilesSubdir holds profiles made with `nixos-rebuild --profile-name`.
const systemProfilesSubdir = "system-profiles"

// staleProfileAge is when a named profile starts looking abandoned rather
// than merely unused. Two months is short enough to catch an experiment from
// the start of the summer, long enough not to nag about one from last week.
const staleProfileAge = 60 * 24 * time.Hour

var genLinkRe = regexp.MustCompile(`^system-(\d+)-link$`)
var namedGenLinkRe = regexp.MustCompile(`^(.+)-(\d+)-link$`)

// namedProfile is a system profile other than the default one.
type namedProfile struct {
	name    string
	newest  int
	updated time.Time
}

func (p namedProfile) age() time.Duration {
	return time.Since(p.updated)
}

// checkGenerations compares the system that was built with the one the
// GRUB menu actually boots.
//
// These come apart when a rebuild succeeds but installing the bootloader
// does not: every file is in place, nothing looks broken, and yet the next
// reboot lands in the previous system.
func checkGenerations(e *env) Check {
	const name = "generations"

	nums, err := listGenerations(e.profilesDir)
	if err != nil {
		return skip(name, "%v", err)
	}
	if len(nums) == 0 {
		return skip(name, "no generations found in %s", e.profilesDir)
	}

	built, err := currentSystem(e.profilesDir)
	if err != nil {
		return skip(name, "%v", err)
	}
	inMenu, err := defaultSystemPath(e.cfgPath)
	if err != nil {
		return skip(name, "%v", err)
	}
	if abs, err := filepath.Abs(inMenu); err == nil {
		inMenu = abs
	}

	layout, layoutErr := readMenuLayout(e.cfgPath)
	profiles := listNamedProfiles(e.profilesDir)

	withCounts := func(c Check) Check {
		c = c.data("generations", len(nums)).data("newest", nums[len(nums)-1])
		if layoutErr == nil {
			c = c.data("menu_entries", layout.main).
				data("menu_profile_entries", layout.profileTotal())
		}
		return c
	}

	if built != inMenu {
		return withCounts(fail(name, "the bootloader is out of date with the built system")).
			detail("built:   %s", built).
			detail("in menu: %s", inMenu).
			detail("the rebuild went through but the bootloader was not updated").
			detail("rebooting now lands in the previous system")
	}

	c := withCounts(ok(name, "%s, menu matches the built system",
		plural(len(nums), "generation", "generations")))
	c = c.detail("newest generation: %d", nums[len(nums)-1])

	// Only some generations reach the menu: configurationLimit decides how
	// many are listed. The rest stay in the store, reachable through
	// nixos-rebuild but not at boot time.
	if layoutErr == nil && layout.main > 0 {
		c = c.detail("%s in the menu, out of %d kept",
			plural(layout.main, "entry", "entries"), len(nums))
	}

	// Named profiles get their own submenu and their own limit, so the
	// entry count can exceed configurationLimit without anything being wrong.
	if layoutErr == nil && len(layout.profiles) > 0 {
		c = c.detail("plus %s in %s: %s",
			plural(layout.profileTotal(), "entry", "entries"),
			plural(len(layout.profiles), "other profile", "other profiles"),
			strings.Join(layout.profileNames(), ", "))
	}

	// A named profile nobody has touched in months is usually an experiment
	// that was never cleaned up. It holds a system in the store and takes a
	// slot in the boot menu.
	var forgotten []namedProfile
	for _, p := range profiles {
		if p.age() > staleProfileAge {
			forgotten = append(forgotten, p)
		}
	}
	if len(forgotten) > 0 {
		c = warn(name, "%s untouched for months",
			plural(len(forgotten), "profile has been", "profiles have been"))
		c = withCounts(c)
		for _, p := range forgotten {
			c = c.detail("%s, generation %d, last built %s",
				p.name, p.newest, humanAge(p.age()))
		}
		c = c.detail("each keeps a system in the store and an entry in the boot menu")
		c = c.detail("remove with: sudo rm %s",
			filepath.Join(e.profilesDir, systemProfilesSubdir, "NAME*"))
		c = c.data("stale_profiles", len(forgotten))
		return c
	}

	// Plenty of generations is not a fault, but the partition is finite.
	if len(nums) > 40 {
		c = warn(name, "%s are kept, each one pins a system in the store",
			plural(len(nums), "generation", "generations"))
		c = withCounts(c).
			detail("clean up with: sudo nix-collect-garbage --delete-older-than 30d")
	}
	return c
}

// listNamedProfiles reads the profiles created with --profile-name.
func listNamedProfiles(profilesDir string) []namedProfile {
	dir := filepath.Join(profilesDir, systemProfilesSubdir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	newest := map[string]int{}
	when := map[string]time.Time{}

	for _, entry := range entries {
		m := namedGenLinkRe.FindStringSubmatch(entry.Name())
		if m == nil {
			continue // the bare NAME link, which points at one of these
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if n <= newest[m[1]] {
			continue
		}
		newest[m[1]] = n
		if info, err := entry.Info(); err == nil {
			when[m[1]] = info.ModTime()
		}
	}

	out := make([]namedProfile, 0, len(newest))
	for name, n := range newest {
		out = append(out, namedProfile{name: name, newest: n, updated: when[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// humanAge turns a duration into something worth reading in a report.
func humanAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days >= 365:
		return fmt.Sprintf("about %s ago", plural(days/365, "year", "years"))
	case days >= 60:
		return fmt.Sprintf("about %s ago", plural(days/30, "month", "months"))
	case days >= 1:
		return fmt.Sprintf("%s ago", plural(days, "day", "days"))
	default:
		return "today"
	}
}

// currentSystem resolves the profile symlink to the store path it points at.
func currentSystem(profilesDir string) (string, error) {
	target, err := os.Readlink(filepath.Join(profilesDir, "system"))
	if err != nil {
		return "", fmt.Errorf("cannot read the system profile link: %w", err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(profilesDir, target)
	}
	if real, err := filepath.EvalSymlinks(target); err == nil {
		target = real
	}
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	return target, nil
}

// listGenerations returns the system generation numbers in ascending order.
func listGenerations(profilesDir string) ([]int, error) {
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s not found, this may not be a NixOS system", profilesDir)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("no permission to read %s, try running with sudo", profilesDir)
		}
		return nil, err
	}
	var nums []int
	for _, entry := range entries {
		m := genLinkRe.FindStringSubmatch(entry.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums, nil
}
