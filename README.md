# bootcheck

Tells you whether a NixOS machine will come back up **before** you reboot it.

A broken bootloader only announces itself once the system is already down, which
is the worst possible moment: no shell, no logs, no package manager. `bootcheck`
asks the same questions the firmware and GRUB will ask, while you can still fix
the answer.

Everything it does is read only. It opens files and follows symlinks; nothing on
disk is modified.

## What it checks

| Check | Question |
| --- | --- |
| `kernels` | Does every kernel and initrd the GRUB menu points at actually exist? |
| `uefi` | Is the bootloader entry still in the firmware, enabled, and first in `BootOrder`? |
| `generations` | Does the menu boot the system that was last built, and is any named profile abandoned? |
| `space` | Is there room on the EFI partition for a few more generations? |
| `integrity` | Are there traces of a past filesystem repair, or config from a bootloader you dropped? |
| `leftovers` | Are kernels from another bootloader still taking up space? |
| `orphans` | Are there kernels on the partition that nothing references? |

The `generations` check is the one that is hard to spot by hand. A rebuild can
succeed while installing the bootloader quietly does not, leaving a machine that
looks perfectly healthy and boots into last week's system.

It also explains the boot menu. The number of entries rarely matches
`configurationLimit`, because the default entry duplicates the newest
generation and every named profile from `nixos-rebuild --profile-name` gets its
own submenu and its own limit. A profile nobody has rebuilt in two months is
flagged: it is usually an experiment that was never cleaned up, still pinning a
system in the store and a slot in the menu.

## Usage

```
sudo bootcheck
```

Root is required: on most NixOS installs the EFI partition is mounted with
`dmask=0077`, and UEFI variables are root-only too. Without it you get one
line saying so, not a wall of failures: a check that cannot look has not
found trouble, and saying otherwise would send someone hunting for a problem
that is not there.

When everything passes you get one line:

```
[ok]   all 7 checks passed, safe to reboot
```

When something needs attention, only that something is printed:

```
[warn] 2 files in /boot/kernels are not referenced by any menu entry, 37.7 MiB
       yyy-initrd-linux-6.12.74-initrd
       zzz-linux-6.12.74-bzImage
       left over from generations that no longer exist

[warn] should boot fine, but 1 check is worth a look
```

### Flags

| Flag | Meaning |
| --- | --- |
| `-v` | print every check, including the ones that passed |
| `-json` | print the whole report as JSON |
| `-boot` | mount point of the EFI partition (default `/boot`) |
| `-config` | path to `grub.cfg` (default `BOOT/grub/grub.cfg`) |
| `-efivars` | UEFI variable directory (default `/sys/firmware/efi/efivars`) |
| `-profiles` | system generation directory (default `/nix/var/nix/profiles`) |
| `-loader` | substring of the UEFI entry name to look for (default `NixOS`) |
| `-version` | print the version and exit |

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | safe to reboot, warnings may still be present |
| `1` | at least one check failed |
| `2` | not enough permission to look, or the report could not be written |

Warnings do not fail the run, so `bootcheck` can be chained after a rebuild
without blocking on cosmetic issues.

## Install

### NixOS with flakes

Drop the sources into your configuration repository, then add the package:

```nix
{ pkgs, ... }:
{
  environment.systemPackages = [
    (pkgs.callPackage ../../pkgs/bootcheck { })
  ];
}
```

Flakes only see files tracked by git, so `git add` the directory before
rebuilding.

### Anywhere else

```
go build -o bootcheck .
```

There are no dependencies beyond the Go standard library.

## Running it automatically

Chaining it after a rebuild means a broken bootloader surfaces immediately
rather than at the next reboot:

```
nixos-rebuild switch && sudo bootcheck
```

## Scope

Written for NixOS with GRUB on a UEFI machine, because that is where it was
needed. The `uefi`, `space` and `integrity` checks are not NixOS specific, but
`kernels`, `generations`, `leftovers` and `orphans` assume the NixOS layout:
kernels under `/boot/kernels`, generations under `/nix/var/nix/profiles`, and
`init=` pointing into the Nix store.

systemd-boot is recognised only as something to clean up after, not as a
bootloader to check.

## License

MIT

Built for NixOS with GRUB on UEFI.
