package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// defaultEntryTitle is the name NixOS gives the entry that boots without
// any choice from the user.
const defaultEntryTitle = "NixOS"

// ref is one file the GRUB menu points at.
type ref struct {
	path    string   // path relative to the EFI partition
	entries []string // menu entries pointing at it
}

// checkKernels verifies that every file the GRUB menu references is really
// on disk. A missing file means GRUB drops to its rescue prompt.
func checkKernels(e *env) Check {
	const name = "kernels"

	if e.refsErr != nil {
		return fail(name, "cannot read the GRUB config: %v", e.refsErr)
	}
	if len(e.refs) == 0 {
		return fail(name, "no linux or initrd lines found in %s", e.cfgPath)
	}

	var missing []ref
	defaultBroken := false
	for _, r := range e.refs {
		if _, err := os.Stat(filepath.Join(e.bootDir, r.path)); err != nil {
			missing = append(missing, r)
			if contains(r.entries, defaultEntryTitle) {
				defaultBroken = true
			}
		}
	}

	if len(missing) == 0 {
		c := ok(name, "GRUB references %s, all present",
			plural(len(e.refs), "file", "files"))
		for _, r := range e.refs {
			c = c.detail("%s (%s)", filepath.Join(e.bootDir, r.path),
				plural(len(r.entries), "entry", "entries"))
		}
		return c.data("referenced", len(e.refs))
	}

	c := fail(name, "%d of %s referenced by GRUB are missing",
		len(missing), plural(len(e.refs), "file", "files"))
	for _, r := range missing {
		c = c.detail("missing: %s", filepath.Join(e.bootDir, r.path))
		c = c.detail("referenced by: %s", shortList(sorted(r.entries), 3))
	}
	if defaultBroken {
		c = c.detail("the default menu entry is broken, the machine will drop to the grub> prompt")
	} else {
		c = c.detail("the default entry is intact, but older generations will not boot")
	}
	return c.data("missing", len(missing)).data("referenced", len(e.refs))
}

// parseGrubConfig reads grub.cfg and returns the unique files it points at,
// keeping the order in which they first appear.
func parseGrubConfig(path string) ([]ref, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("no permission to read %s, try running with sudo", path)
		}
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s not found, pass -config", path)
		}
		return nil, err
	}
	defer f.Close()

	var (
		order   []string
		byPath  = map[string]*ref{}
		current = "(outside any menu entry)"
	)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())

		if title, isEntry := menuTitle(line); isEntry {
			current = title
			continue
		}

		raw, isFile := fileArg(line)
		if !isFile {
			continue
		}
		p := espPath(raw)
		if p == "" {
			continue
		}

		r, seen := byPath[p]
		if !seen {
			r = &ref{path: p}
			byPath[p] = r
			order = append(order, p)
		}
		if !contains(r.entries, current) {
			r.entries = append(r.entries, current)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	out := make([]ref, 0, len(order))
	for _, p := range order {
		out = append(out, *byPath[p])
	}
	return out, nil
}

// menuTitle extracts the label from a menuentry "..." line.
func menuTitle(line string) (string, bool) {
	if !strings.HasPrefix(line, "menuentry") {
		return "", false
	}
	start := strings.Index(line, "\"")
	if start < 0 {
		return "", false
	}
	end := strings.Index(line[start+1:], "\"")
	if end < 0 {
		return "", false
	}
	return line[start+1 : start+1+end], true
}

// fileArg returns the first argument of a linux or initrd line.
// From `linux ($drive1)//kernels/abc init=...` it returns `($drive1)//kernels/abc`.
func fileArg(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", false
	}
	switch fields[0] {
	case "linux", "linux16", "linuxefi", "initrd", "initrd16", "initrdefi":
		return fields[1], true
	}
	return "", false
}

// espPath turns a GRUB path into one relative to the EFI partition,
// dropping the drive in parentheses and collapsing repeated slashes.
func espPath(raw string) string {
	s := raw
	if strings.HasPrefix(s, "(") {
		i := strings.Index(s, ")")
		if i < 0 {
			return ""
		}
		s = s[i+1:]
	}
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	if s == "" || s == "/" {
		return ""
	}
	if !strings.HasPrefix(s, "/") {
		s = "/" + s
	}
	return s
}

// initPathRe pulls the system path out of init=/nix/store/HASH-.../init.
var initPathRe = regexp.MustCompile(`(?:^|\s)init=(\S+)/init(?:\s|$)`)

// defaultSystemPath returns the store path the first menu entry boots,
// that is the one used when nobody touches the keyboard.
func defaultSystemPath(cfg string) (string, error) {
	f, err := os.Open(cfg)
	if err != nil {
		return "", fmt.Errorf("cannot read %s: %w", cfg, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	inFirst := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if _, isEntry := menuTitle(line); isEntry {
			if inFirst {
				break // the first entry ended, stop looking
			}
			inFirst = true
			continue
		}
		if !inFirst {
			continue
		}
		if m := initPathRe.FindStringSubmatch(line); m != nil {
			return m[1], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("the first menu entry has no init= parameter")
}

// menuEntryCount counts the entries that boot a system generation.
// The very first entry is skipped: it duplicates the newest generation.
func menuEntryCount(cfg string) (int, error) {
	f, err := os.Open(cfg)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	n := 0
	for sc.Scan() {
		title, isEntry := menuTitle(strings.TrimSpace(sc.Text()))
		if isEntry && strings.Contains(title, "Configuration") {
			n++
		}
	}
	return n, sc.Err()
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
