package main

import (
	"errors"
	"io/fs"
	"os"
)

// isPermissionProblem reports whether an error is really about access rights.
//
// Missing permission is not a broken bootloader: it means the check could not
// look, not that it looked and found trouble. Reporting it as a failure would
// tell someone not to reboot a machine that is perfectly fine.
func isPermissionProblem(err error) bool {
	return err != nil && errors.Is(err, fs.ErrPermission)
}

// needsRoot reports whether the parts of the system these checks read are
// out of reach for the current user.
//
// The EFI partition is normally mounted with dmask=0077 and the UEFI
// variables are root-only, so almost nothing works without elevation.
func needsRoot(e *env) bool {
	if os.Geteuid() == 0 {
		return false
	}
	f, err := os.Open(e.cfgPath)
	if err == nil {
		f.Close()
		return false
	}
	return isPermissionProblem(err)
}
