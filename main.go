// bootcheck tells you whether the machine will come back up after a reboot.
//
// It runs seven checks against the EFI system partition, the GRUB config,
// the UEFI boot variables and the Nix system profile. Everything is read
// only: nothing on disk is modified.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const version = "0.6.0"

// env holds everything the checks need, parsed once.
type env struct {
	bootDir     string
	cfgPath     string
	varsDir     string
	profilesDir string
	loaderName  string

	refs    []ref // files referenced by the GRUB menu
	refsErr error
}

func main() {
	var (
		bootDir  = flag.String("boot", "/boot", "mount point of the EFI system partition")
		cfgPath  = flag.String("config", "", "path to grub.cfg (defaults to BOOT/grub/grub.cfg)")
		varsDir  = flag.String("efivars", efiVarsDir, "directory holding the UEFI variables")
		profiles = flag.String("profiles", defaultProfilesDir, "directory holding the system generations")
		loader   = flag.String("loader", "NixOS", "substring of the UEFI entry name to look for")
		verbose  = flag.Bool("v", false, "print every check, not just the ones that need attention")
		asJSON   = flag.Bool("json", false, "print the report as JSON")
		showVer  = flag.Bool("version", false, "print the version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("bootcheck " + version)
		return
	}

	e := &env{
		bootDir:     *bootDir,
		cfgPath:     *cfgPath,
		varsDir:     *varsDir,
		profilesDir: *profiles,
		loaderName:  *loader,
	}
	if e.cfgPath == "" {
		e.cfgPath = filepath.Join(e.bootDir, "grub", "grub.cfg")
	}
	e.refs, e.refsErr = parseGrubConfig(e.cfgPath)

	report := buildReport(version, []Check{
		checkKernels(e),
		checkUEFI(e),
		checkGenerations(e),
		checkSpace(e),
		checkIntegrity(e),
		checkLeftovers(e),
		checkOrphans(e),
	})

	if *asJSON {
		if err := report.writeJSON(os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	} else {
		report.writeText(os.Stdout, *verbose)
	}

	if report.Status == StatusFail {
		os.Exit(1)
	}
}
