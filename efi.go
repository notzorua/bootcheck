package main

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// efiVarsDir is where the kernel exposes UEFI variables as plain files.
const efiVarsDir = "/sys/firmware/efi/efivars"

// globalGUID identifies the standard set of boot variables. File names look
// like BootOrder-8be4df61-93ca-11d2-aa0d-00e098032b8c.
const globalGUID = "8be4df61-93ca-11d2-aa0d-00e098032b8c"

// loadOptionActive is the "entry is enabled" bit of a boot option.
const loadOptionActive = 0x00000001

var bootVarRe = regexp.MustCompile(`^Boot([0-9A-Fa-f]{4})-` + globalGUID + `$`)

// bootEntry is one boot option stored in the firmware.
type bootEntry struct {
	num    uint16
	desc   string
	active bool
}

func (b bootEntry) label() string {
	return fmt.Sprintf("Boot%04X", b.num)
}

// checkUEFI asks whether the firmware will still find the bootloader.
func checkUEFI(e *env) Check {
	const name = "uefi"

	if _, err := os.Stat(e.varsDir); err != nil {
		if os.IsNotExist(err) {
			return skip(name, "%s is missing, this machine did not boot via UEFI", e.varsDir)
		}
		return fail(name, "cannot read %s: %v", e.varsDir, err)
	}

	entries, err := readBootEntries(e.varsDir)
	if err != nil {
		if isPermissionProblem(err) {
			return skip(name, "%v", err)
		}
		return fail(name, "%v", err)
	}
	order, err := readBootOrder(e.varsDir)
	if err != nil {
		if isPermissionProblem(err) {
			return skip(name, "%v", err)
		}
		return fail(name, "%v", err)
	}
	if len(order) == 0 {
		return fail(name, "BootOrder is empty, the firmware has nothing to boot")
	}

	// The full boot order, shown in verbose mode or when something is off.
	var lines []string
	for i, num := range order {
		entry, known := entries[num]
		switch {
		case !known:
			lines = append(lines, fmt.Sprintf("#%d Boot%04X, no such entry exists", i+1, num))
		case !entry.active:
			lines = append(lines, fmt.Sprintf("#%d %s %q (disabled)", i+1, entry.label(), entry.desc))
		default:
			lines = append(lines, fmt.Sprintf("#%d %s %q", i+1, entry.label(), entry.desc))
		}
	}

	pos, found := -1, bootEntry{}
	for i, num := range order {
		entry, known := entries[num]
		if known && strings.Contains(strings.ToLower(entry.desc), strings.ToLower(e.loaderName)) {
			pos, found = i, entry
			break
		}
	}

	withOrder := func(c Check) Check {
		for _, l := range lines {
			c = c.detail("%s", l)
		}
		return c.data("boot_order", lines)
	}

	if pos < 0 {
		// The entry may exist but have fallen out of BootOrder.
		for _, entry := range entries {
			if strings.Contains(strings.ToLower(entry.desc), strings.ToLower(e.loaderName)) {
				return withOrder(fail(name, "entry %s %q exists but is not listed in BootOrder",
					entry.label(), entry.desc)).
					detail("the firmware will never try it, the boot order needs fixing")
			}
		}
		return withOrder(fail(name, "no boot entry matching %q", e.loaderName))
	}

	if !found.active {
		return withOrder(fail(name, "entry %s %q is disabled", found.label(), found.desc))
	}

	if pos > 0 {
		first := "an unknown entry"
		if entry, known := entries[order[0]]; known {
			first = fmt.Sprintf("%q", entry.desc)
		}
		return withOrder(warn(name, "%q is #%d in BootOrder, %s comes first", found.desc, pos+1, first)).
			detail("the order lives in the firmware setup, under the boot priority list")
	}

	return withOrder(ok(name, "entry %s %q found, first in BootOrder", found.label(), found.desc))
}

// readBootOrder returns the entry numbers in the order the firmware tries them.
func readBootOrder(varsDir string) ([]uint16, error) {
	data, err := readVar(varsDir, "BootOrder")
	if err != nil {
		return nil, err
	}
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("BootOrder has an odd length of %d bytes", len(data))
	}
	out := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		out = append(out, binary.LittleEndian.Uint16(data[i:i+2]))
	}
	return out, nil
}

// readBootEntries collects every Boot#### variable.
func readBootEntries(varsDir string) (map[uint16]bootEntry, error) {
	names, err := os.ReadDir(varsDir)
	if err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("no permission to read %s: %w", varsDir, fs.ErrPermission)
		}
		return nil, err
	}

	out := map[uint16]bootEntry{}
	for _, n := range names {
		m := bootVarRe.FindStringSubmatch(n.Name())
		if m == nil {
			continue
		}
		num, err := strconv.ParseUint(m[1], 16, 16)
		if err != nil {
			continue
		}
		data, err := readVar(varsDir, "Boot"+m[1])
		if err != nil {
			continue // one unreadable entry is not worth failing over
		}
		desc, active, err := parseLoadOption(data)
		if err != nil {
			continue
		}
		out[uint16(num)] = bootEntry{num: uint16(num), desc: desc, active: active}
	}
	return out, nil
}

// readVar reads a UEFI variable and strips the four attribute bytes that
// efivarfs puts in front of every value.
func readVar(varsDir, name string) ([]byte, error) {
	path := filepath.Join(varsDir, name+"-"+globalGUID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("no permission to read %s: %w", path, fs.ErrPermission)
		}
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("variable %s not found", name)
		}
		return nil, err
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("variable %s is only %d bytes long", name, len(data))
	}
	return data[4:], nil
}

// parseLoadOption decodes an EFI_LOAD_OPTION structure. Layout: four bytes
// of attributes, two bytes of device path length, then a NUL terminated
// UTF-16 description, then the path to the loader itself.
func parseLoadOption(data []byte) (desc string, active bool, err error) {
	if len(data) < 6 {
		return "", false, fmt.Errorf("boot option shorter than six bytes")
	}
	attrs := binary.LittleEndian.Uint32(data[0:4])
	active = attrs&loadOptionActive != 0

	rest := data[6:]
	var units []uint16
	for i := 0; i+1 < len(rest); i += 2 {
		u := binary.LittleEndian.Uint16(rest[i : i+2])
		if u == 0 {
			return string(utf16.Decode(units)), active, nil
		}
		units = append(units, u)
	}
	return "", false, fmt.Errorf("boot option description is not NUL terminated")
}
