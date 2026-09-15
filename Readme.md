# PXE Installer Agent 
[![wakatime](https://wakatime.com/badge/user/bbc34f7e-9b97-4141-9f4c-64ed61b82c61/project/1b641e6e-4b98-40c8-b667-8ff44b7ad006.svg)](https://wakatime.com/badge/user/bbc34f7e-9b97-4141-9f4c-64ed61b82c61/project/1b641e6e-4b98-40c8-b667-8ff44b7ad006)

# netboot-provisioner

A Go-based bare-metal provisioning tool. It boots machines over PXE, installs an OS unattended via kickstart, debian-installer preseed, or similar, then hands off to Ansible to deploy k3s.

## Problem

Bare-metal provisioning tools either assume a specific distro's installer ecosystem or a much heavier operator model than a small cluster needs. This project targets the narrow case: PXE boot a machine, run an unattended OS install, then invoke Ansible for k3s bring-up. One Go binary plus a CSV-backed inventory, not a stack of daemons.

## Architecture

1. Machine powers on, NIC broadcasts a DHCP request.
2. Built-in DHCP server answers, using a map-keyed client lookup for O(1) resolution against the inventory.
3. TFTP serves the bootloader. Legacy BIOS PXE and UEFI/EFI netboot are both supported.
4. HTTP serves the kernel, initrd, and the per-client install config (kickstart, preseed, or equivalent), templated per OS type.  5. The install runs unattended.
6. On completion, a single-use systemd service calls back to the HTTP server with the machine's MAC address.
7. The provisioner regenerates that machine's GRUB config so the next boot goes to local disk instead of PXE, and triggers an Ansible playbook to install k3s.

```
[PXE client] -> [DHCP] -> [TFTP: bootloader] -> [HTTP: kernel/initrd/install config]
                                                        |
                                                  [Unattended OS install]
                                                        |
                                       [systemd callback: MAC -> HTTP endpoint]
                                                        |
                                  [GRUB regenerated for local boot] + [Ansible: k3s install]
```

## Features

- DHCP, TFTP, and HTTP netboot services in one binary
- Map-keyed client inventory for O(1) DHCP lookup, backed by a CSV file
- File watcher (fsnotify) on the inventory file, changes trigger automatic repopulation
- HTTP POST endpoint to register new PXE clients (mac, ip, ostype), appended to inventory automatically
- Per-client pxelinux.cfg and EFI/GRUB config generation, templated by OS type
- Legacy BIOS PXE and UEFI/EFI netboot support
- Tested install flows: Debian, Ubuntu (including 26.04), CentOS Stream, RHEL 9.2
- Post-install callback regenerates GRUB config so the machine boots to local disk afterward
- Ansible integration (go-ansible v2) to deploy k3s after install
- Early Kubernetes operations support: fetch node list and resource usage from a provisioned cluster (MVP, cluster-scope only)

## Requirements

- Go 1.2x or later
- Ansible installed on the provisioner host
- A network segment where this tool can run DHCP without conflicting with an existing DHCP server
- Target machines with PXE-capable NICs, UEFI or legacy BIOS

## Asset preparation

The provisioner does not embed OS install artifacts, kernels, initrds, or bootloaders, they must be populated into `assets/` before first use. This is a one-time (or periodic, for updates) step, separate from running the binary.

```bash
make build_structure   # create asset directories, fetch CentOS Stream boot files
make populate_kernel   # fetch Debian, CentOS Stream, and Ubuntu kernels/initrds
make populate_pxe      # extract BIOS pxelinux and EFI GRUB boot files
make populate_repo     # download full Ubuntu and CentOS install ISOs
```

These use `podman` to extract files from official container images and ISOs rather than requiring those tools installed on the host directly. `wget` is used for direct kernel/initrd downloads.

## Local testing without physical hardware

`scripts/createDummyInterface.sh` sets up a bridge and veth pair so you can exercise the DHCP/TFTP/HTTP flow against a local VM without dedicated PXE hardware:

```bash
sudo scripts/createDummyInterface.sh create br0
sudo scripts/createDummyInterface.sh show
sudo scripts/createDummyInterface.sh clean
```

## Asset layout

```
assets/
├── ansible/               # kubeconfig and playbook inputs for target clusters
├── http/
│   ├── centos/
│   │   ├── mirror/        # full CentOS Stream repo mirror (BaseOS, AppStream)
│   │   └── <install config per client, naming not yet unified, see note below>
│   ├── debian/
│   │   └── preseed-<mac>.cfg
│   └── ubuntu/
│       ├── iso/           # cached Ubuntu live-server ISO
│       └── <cloud-init config per client, naming not yet unified, see note below>
├── keys/                  # SSH keypair used by Ansible to reach provisioned targets
└── tftp/
    ├── bios/
    │   ├── pxelinux.0, ldlinux.c32, menu.c32, libutil.c32
    │   └── pxelinux.cfg/01-<mac>   # per-client BIOS PXE config
    ├── efi/
    │   ├── grub/           # GRUB netboot directory (grub-mknetdir output)
    │   └── shimx64.efi(.signed)
    └── images/
        ├── centos/  {vmlinuz, initrd.img}
        ├── debian/  {linux, initrd.gz}
        └── ubuntu/  {linux, initrd}
```

Setup order:

```bash
make build_structure   # create the directory tree above
make generate_keys     # generate assets/keys/test_provisioner if not present
make populate_pxe      # extract BIOS pxelinux + EFI GRUB/shim files
make populate_kernel   # fetch kernels/initrds into assets/tftp/images/
make populate_repo     # fetch full install ISOs and CentOS mirror into assets/http/
```

Per-client files, kickstart, preseed, cloud-init, and the matching `pxelinux.cfg/01-<mac>` entry, are generated at runtime when a client is registered via the HTTP POST endpoint, not by the Makefile.

## Quickstart

```bash
git clone https://github.com/motangpuar/o2-ims-worker.git
cd o2-ims-worker
go build -o bin/worker ./cmd/...
sudo DHCP_BIND_INTERFACE=br0 bin/worker
```

`sudo` is required because DHCP binds to port 67 and TFTP to port 69, both privileged ports.

## Demo: testing with QEMU/KVM

For local testing without physical hardware, use `scripts/createDummyInterface.sh` to create a bridge, then `scripts/create-vm.sh` to define and start a UEFI guest attached to it.

```bash
sudo scripts/createDummyInterface.sh create br0
sudo scripts/create-vm.sh
```

`create-vm.sh` generates the VM's libvirt XML via `virt-install --print-xml`, then patches it to enable OVMF/UEFI firmware with Secure Boot explicitly disabled (faster iteration, not representative of the Secure Boot boot path), attaches it to `br0` via a virtio NIC, and boots it in network-first order so it PXE boots against your running provisioner. VNC is exposed on all interfaces for console access.

```bash
virsh vncdisplay centos-stream-10-vm   # find the VNC display number
# connect with any VNC client to <host-ip>:<display>
```

To tear down and retry:

```bash
sudo virsh destroy centos-stream-10-vm
sudo virsh undefine centos-stream-10-vm --nvram
sudo rm -f /var/lib/libvirt/images/centos-stream-10.qcow2
```

**Current limitation:** the script is hardcoded to one VM name, one disk path, and `os-variant centos-stream9`, so it is CentOS-specific as written despite the generic filename. Testing Debian or Ubuntu installs means editing the script directly, or writing OS-specific variants.

## Configuration

No config file. Everything is set via environment variables, with two CLI flags for Kubernetes access.

CLI flags:

| Flag           | Default           | Purpose                                   |
| ---            | ---               | ---                                       |
| `--kubeconfig` | `/tmp/admin.conf` | Path to kubeconfig for cluster operations |
| `--insecure`   | `false`           | Skip TLS verification against the cluster |

Environment variables, TFTP:

| Variable         | Default   | Purpose                        |
| ---              | ---       | ---                            |
| `TFTP_ENABLE`    | `true`    | Enable/disable the TFTP server |
| `TFTP_BIND_ADDR` | `0.0.0.0` | Bind address                   |
| `TFTP_BIND_PORT` | `69`      | Bind port                      |
| `TFTP_BLOCKSIZE` | `512`     | TFTP block size                |

Environment variables, DHCP:

| Variable | Default | Purpose |
| ---                   | ---           | ---                                      |
| `DHCP_ENABLE`         | `true`        | Enable/disable the DHCP server           |
| `DHCP_MODE`           | `""` (unset)  | Currently unused, see note below         |
| `DHCP_BIND_ADDRESS`   | `0.0.0.0`     | Bind address                             |
| `DHCP_BIND_PORT`      | `67`          | Bind port                                |
| `DHCP_BIND_INTERFACE` | `eth0`        | Interface to bind and listen on          |
| `DHCP_TFTP_IP`        | `192.168.1.1` | TFTP server IP advertised to clients     |
| `DHCP_TFTP_PORT`      | `69`          | TFTP server port advertised to clients   |
| `DHCP_NEXT_SERVER_IP` | `192.168.1.1` | Next-server IP in DHCP replies           |
| `DHCP_BOOT_FILE`      | `pxelinux.0`  | Boot file path advertised to PXE clients |

Inventory is separate from this env-based config. It's the MAC-keyed CSV file watched by fsnotify, not an environment variable.

## Project structure

```
internal/
  inventory/   # MAC-keyed client inventory (filedata), CSV-backed, fsnotify-watched
  dhcp/        # DHCP server, nclient4-based
  tftp/        # TFTP server
  http/        # HTTP server: static netboot assets + client registration endpoint
  db/          # File-backed lease cache
  metrics/     # Prometheus metrics
  k8s/         # Kubernetes node/resource queries and go-ansible k3s orchestration
  system/      # First-boot callback handling, GRUB regeneration
```

## Known limitations / open work

- OS-specific install parameters still live in an in-memory struct extended from the CSV inventory, not a static spec file yet
- Kubernetes operations are MVP only: node and resource fetch works, cluster-scope only, not yet cleanly separated from provisioning concerns
- No automated fetch scripts yet for Ubuntu cloud-init or RHEL ignition assets, current OS support is kickstart/preseed style installers only
- RHEL 9.5 failed in testing, only 9.2 is confirmed working
- Single DHCP server assumed, no HA/failover

## Testing notes

- Debian and Ubuntu 26.04 full install flows tested end to end
- CentOS Stream install tested on physical hardware
- RHEL 9.2 tested successfully on an Intel NUC, RHEL 9.5 failed
- Deployed and tested on a remote subnet (192.168.8.0/24)

