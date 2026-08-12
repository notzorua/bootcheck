{ lib, buildGoModule }:

buildGoModule {
  pname = "bootcheck";
  version = "0.6.0";

  src = ./.;

  # Nothing outside the Go standard library is used.
  vendorHash = null;

  # Drop the symbol table and debug info, roughly halves the binary.
  ldflags = [ "-s" "-w" ];

  meta = with lib; {
    description = "Check whether a NixOS machine will boot before you reboot it";
    longDescription = ''
      Checks seven things before a reboot: the kernels the GRUB menu points at
      exist, the bootloader entry is still present and first in the UEFI boot
      order, the menu boots the system that was actually built, the EFI
      partition has room left, there are no traces of a past filesystem
      repair, and no kernels are left behind by unused bootloaders.
    '';
    license = licenses.mit;
    platforms = platforms.linux;
    mainProgram = "bootcheck";
  };
}
