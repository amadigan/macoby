# Bugs
- Remove railyard.sock from launchd activation
- Handle exit codes properly for vm enable and disable
- Add better logging to vm enable and disable

# Incomplete Features
- Daemon API
- GitHub Actions CI
- localpkg script
- Daemon control CLI
- Fix shutdown in vm debug
- Exclude disks from Time Machine

# Post-MVP Features
- IPv6 support (SLAAC)
- broadcast ports via mDNS

# Issues

## Kernel

The kernel is currently locked at <6.11 due to rosetta requirements.
