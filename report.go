package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Status is the outcome of a single check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
)

// Check is what one inspection found. Details are printed only when the
// check did not pass, or when the user asked for verbose output.
type Check struct {
	Name    string         `json:"name"`
	Status  Status         `json:"status"`
	Summary string         `json:"summary"`
	Details []string       `json:"details,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

func newCheck(name string, st Status, format string, a ...any) Check {
	return Check{Name: name, Status: st, Summary: fmt.Sprintf(format, a...)}
}

func ok(name, format string, a ...any) Check {
	return newCheck(name, StatusOK, format, a...)
}

func warn(name, format string, a ...any) Check {
	return newCheck(name, StatusWarn, format, a...)
}

func fail(name, format string, a ...any) Check {
	return newCheck(name, StatusFail, format, a...)
}

func skip(name, format string, a ...any) Check {
	return newCheck(name, StatusSkip, format, a...)
}

// detail appends an explanatory line to a check.
func (c Check) detail(format string, a ...any) Check {
	c.Details = append(c.Details, fmt.Sprintf(format, a...))
	return c
}

// data attaches a machine readable value, used by the JSON output.
func (c Check) data(key string, value any) Check {
	if c.Data == nil {
		c.Data = map[string]any{}
	}
	c.Data[key] = value
	return c
}

// Report is the result of the whole run.
type Report struct {
	Version      string  `json:"version"`
	Status       Status  `json:"status"`
	Summary      string  `json:"summary"`
	SafeToReboot bool    `json:"safe_to_reboot"`
	Checks       []Check `json:"checks"`
}

func buildReport(version string, checks []Check) Report {
	r := Report{Version: version, Status: StatusOK, Checks: checks}

	needAttention := 0
	for _, c := range checks {
		switch c.Status {
		case StatusFail:
			r.Status = StatusFail
			needAttention++
		case StatusWarn:
			if r.Status != StatusFail {
				r.Status = StatusWarn
			}
			needAttention++
		}
	}

	switch r.Status {
	case StatusFail:
		r.SafeToReboot = false
		r.Summary = fmt.Sprintf("%s failing, do not reboot yet",
			plural(needAttention, "check is", "checks are"))
	case StatusWarn:
		r.SafeToReboot = true
		r.Summary = fmt.Sprintf("should boot fine, but %s worth a look",
			plural(needAttention, "check is", "checks are"))
	default:
		r.SafeToReboot = true
		r.Summary = fmt.Sprintf("all %s passed, safe to reboot",
			plural(len(checks), "check", "checks"))
	}
	return r
}

// writeText prints the report for a human.
//
// When everything is fine and verbose is off, a single line is enough:
// a wall of green text trains people to stop reading it.
func (r Report) writeText(w io.Writer, verbose bool) {
	if r.Status == StatusOK && !verbose {
		fmt.Fprintf(w, "%s %s\n", tag(StatusOK), r.Summary)
		return
	}

	shown := 0
	for _, c := range r.Checks {
		if !verbose && c.Status == StatusOK {
			continue
		}
		if shown > 0 {
			fmt.Fprintln(w)
		}
		shown++
		fmt.Fprintf(w, "%s %s\n", tag(c.Status), c.Summary)
		for _, d := range c.Details {
			fmt.Fprintf(w, "       %s\n", d)
		}
	}

	if shown > 0 {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%s %s\n", tag(r.Status), r.Summary)
}

// writeJSON prints the report for another program.
func (r Report) writeJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func tag(s Status) string {
	switch s {
	case StatusFail:
		return "[FAIL]"
	case StatusWarn:
		return "[warn]"
	case StatusSkip:
		return "[skip]"
	default:
		return "[ok]  "
	}
}

// plural joins a count with the right form of a word.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// human turns a byte count into something readable.
func human(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	names := []string{"KiB", "MiB", "GiB", "TiB"}
	if exp >= len(names) {
		exp = len(names) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(b)/float64(div), names[exp])
}

// shortList joins names, cutting off a long tail.
func shortList(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, "; ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:max], "; "), len(items)-max)
}
