package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

const defaultProfilesDir = "/nix/var/nix/profiles"

var genLinkRe = regexp.MustCompile(`^system-(\d+)-link$`)

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

	// Not every generation reaches the menu: configurationLimit decides
	// how many are listed. The rest stay in the store, reachable through
	// nixos-rebuild but not at boot time.
	menuCount, menuErr := menuEntryCount(e.cfgPath)

	withCounts := func(c Check) Check {
		c = c.data("generations", len(nums)).data("newest", nums[len(nums)-1])
		if menuErr == nil {
			c = c.data("in_menu", menuCount)
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

	summary := fmt.Sprintf("%s, menu matches the built system",
		plural(len(nums), "generation", "generations"))
	if menuErr == nil && menuCount > 0 && menuCount < len(nums) {
		summary = fmt.Sprintf("%s, %d in the GRUB menu, menu matches the built system",
			plural(len(nums), "generation", "generations"), menuCount)
	}

	c := withCounts(ok(name, "%s", summary)).detail("newest generation: %d", nums[len(nums)-1])

	// Plenty of generations is not a fault, but the partition is finite.
	if len(nums) > 40 {
		c = warn(name, "%s are kept, each one pins a system in the store",
			plural(len(nums), "generation", "generations"))
		c = withCounts(c).
			detail("clean up with: sudo nix-collect-garbage --delete-older-than 30d")
	}
	return c
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
