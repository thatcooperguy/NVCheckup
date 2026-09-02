# NVCheckup Diagnostic Report

> NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.

**Generated:** 2026-09-02 10:00:00 | **Mode:** full | **Platform:** linux | **Runtime:** 4.2s

## Summary

```
NVCheckup v0.2.3 | 2026-09-02 10:00:00
OS: Ubuntu 24.04 | Kernel: 6.17.0-1031-nvidia | Arch: arm64
Platform: DGX Spark (Founders Edition) | DGX OS 7.2.3 / OTA 7.5.0
GPU: NVIDIA GB10 | Driver: 580.159.03
Unified memory: 119.7 GiB total, 115.9 GiB available
CUDA (driver): 13.0
Temp: 42°C | P-State: P8 | Util: 0%
PCIe: n/a (on-package, NVLink-C2C)
Findings: 0 CRITICAL, 0 WARNING, 3 total
```

## System

| Property | Value |
|----------|-------|
| OS | Ubuntu 24.04 |
| Kernel | 6.17.0-1031-nvidia |
| Architecture | arm64 |
| CPU | Cortex-X925 / Cortex-A725 (20 cores) |
| RAM | 122572 MB |
| Boot Mode | UEFI |
| Secure Boot | Enabled |
| Uptime | 1d 2h 3m |

## Platform

| Property | Value |
|----------|-------|
| Class | DGX Spark (dgx-spark) |
| Vendor/Model | NVIDIA NVIDIA_DGX_Spark (Founders Edition) [version A.7, BIOS 5.36_0ACUM023] |
| GPU SoC | GB10 (compute capability 12.1) |
| Unified memory | yes (nvidia-smi memory, fan, power limit and PCIe fields are [N/A] by design) |
| DGX OS | image 7.2.3 / OTA 7.5.0 (OTA2607) |
| Memory pool | 119.7 GiB total, 115.9 GiB available, 131.9 GiB allocatable (MemAvailable + SwapFree; HugePages override, spec 3.3) |
| Previous boot | clean shutdown (last line 'systemd-journald[512]: Journal stopped'); pstore empty; 0 log-less boot(s) in the last 14 days |
| Cluster fabric | 2 ConnectX-7 port(s): enp1s0f0np0 4: ACTIVE 200000 Mb/s; enP2p1s0f0np0 4: ACTIVE 200000 Mb/s |

## Unified Memory

| Property | Value |
|----------|-------|
| MemTotal | 119.7 GiB |
| MemAvailable | 115.9 GiB |
| MemFree | 112.6 GiB |
| Buffers + Cached | 3.2 GiB (reclaimable) |
| Swap | 0.0 of 16.0 GiB used (/swapfile) |
| Swappiness | 60 |
| Allocatable | 131.9 GiB |
| PSI memory | some avg10 0.00, full avg10 0.00 |
| GPU processes | 0 |
| OOM events | 0 OOM-killer, 0 NVRM no-memory |

## DGX OS

| Property | Value |
|----------|-------|
| Release | NVIDIA DGX Spark, image 7.2.3 (DGX_SWBUILD_VERSION, built 2025-09-10-13-50-03, commit 833b4a7) |
| OTA | 7.5.0 (DGX_OTA_VERSION) OTA2607, applied Wed Jul 15 09:06:56 AM PDT 2026 |
| DGX platform | DGX Server for KVM |
| FastOS | 1.91.51 |
| Serial | <serial> |
| OTA check | torn=0, failed: N/A |
| Driver package | 580.159.03-0ubuntu0.24.04.1 |
| Firmware package | 580.159.03-0ubuntu0.24.04.1 |
| Modules for kernel | present |
| Dashboard | dgx-dashboard active, dgx-dashboard-admin active, port 11000 open |
| fwupd | active |
| Persistenced | active |

## Firmware

| Property | Value |
|----------|-------|
| Embedded Controller | 3.5.8 |
| UEFI Device Firmware | 2.155.11 |
| USB Power Delivery Controller | 0.5.22 |

## Cluster Fabric (ConnectX-7)

| Property | Value |
|----------|-------|
| enp1s0f0np0 (rocep1s0f0) | cage 0, 4: ACTIVE, 200000 Mb/s, MTU 9000, IPv4 192.168.100.1/24, persistent |
| enP2p1s0f0np0 (roceP2p1s0f0) | cage 0, 4: ACTIVE, 200000 Mb/s, MTU 9000, IPv4 192.168.101.1/24, persistent |
| cx7-hotplug-enabled | yes |
| netplan MTU | 9000 |
| nvidia-peermem | load attempted: no |
| avahi | active, 0 hostname conflict(s) |
| ufw | disabled |
| rdma tools | ibstat, ibdev2netdev, avahi-browse |

## GPUs

### GPU 0: NVIDIA GB10

| Property | Value |
|----------|-------|
| Vendor | NVIDIA |
| Driver | 580.159.03 |
| Memory | unified pool (nvidia-smi reports [N/A]; see Platform) |
| Compute capability | 12.1 |
| Package | on-package GPU (NVLink-C2C to the CPU) |
| Temperature | 42°C |

**NVIDIA Driver:** 580.159.03 | **CUDA (driver):** 13.0

**PCIe:** n/a (on-package, NVLink-C2C)

**Thermal:** 42°C, P8, fan N/A, 9.87 W / limit N/A

## Linux

| Property | Value |
|----------|-------|
| Distro | Ubuntu 24.04 |
| Session | N/A |
| Secure Boot | Enabled |
| DKMS | N/A |
| libcuda.so | /usr/lib/aarch64-linux-gnu/libcuda.so.580.159.03 |
| PRIME | N/A |
| /dev/nvidia* | /dev/nvidia0, /dev/nvidiactl, /dev/nvidia-uvm |
| Xid errors | 0 |

## Findings

| Severity | Finding | Evidence | Next Step |
|----------|---------|----------|-----------|
| **INFO** | NVIDIA DGX Spark (GB10) Detected | GB10 platform: NVIDIA NVIDIA_DGX_Spark (Founders Edition); GPU NVIDIA GB10 CC... | No action; memory comes from /proc/meminfo, nvidia-smi memory fields are expe... |
| **INFO** | Secure Boot Enabled — NVIDIA Module is Loading Successfully | Secure Boot is enabled and the NVIDIA module is loaded. Module signing appear... | No action needed. |
| **INFO** | Unified Memory: nvidia-smi Memory Fields Are [N/A] by Design | nvidia-smi memory '[N/A]' on GPU 0 (NVIDIA GB10) is expected on unified-memor... | Use /proc/meminfo MemAvailable (+SwapFree), free -h 'available' or the DGX Da... |

### Details

<details>
<summary><b>[INFO] #1: NVIDIA DGX Spark (GB10) Detected</b></summary>

**ID:** `dgx-spark-detected`

**Evidence:** GB10 platform: NVIDIA NVIDIA_DGX_Spark (Founders Edition); GPU NVIDIA GB10 CC 12.1; kernel 6.17.0-1031-nvidia; DGX OS 7.2.3 / OTA 7.5.0 (OTA2607).

**Why it matters:** Enables unified-memory suppressions and DGX OS pairing rules; OEM units lag NVIDIA OTAs.

**Next steps:**
- No action; memory comes from /proc/meminfo, nvidia-smi memory fields are expected to be [N/A].

</details>

<details>
<summary><b>[INFO] #2: Secure Boot Enabled — NVIDIA Module is Loading Successfully</b></summary>

**ID:** `secureboot-ok`

**Evidence:** Secure Boot is enabled and the NVIDIA module is loaded. Module signing appears to be properly configured.

**Why it matters:** This is the ideal configuration — security is maintained while NVIDIA drivers function correctly.

**Next steps:**
- No action needed.

</details>

<details>
<summary><b>[INFO] #3: Unified Memory: nvidia-smi Memory Fields Are [N/A] by Design</b></summary>

**ID:** `unified-memory-nvsmi-expected`

**Evidence:** nvidia-smi memory '[N/A]' on GPU 0 (NVIDIA GB10) is expected on unified-memory iGPUs. Pool: MemTotal 119.7 GiB measured from /proc/meminfo; the remainder of the 128 GiB LPDDR5X (marketed as 128 GB) is reserved for display/firmware (~8.3 GiB on 2025 units; the 2 GB / 4 GB display reserve is a BIOS toggle since July 2026, S4). MemAvailable 115.9 GiB, swap 0.0 GiB. Fan, power limit, memory clock and PCIe gen/width are also [N/A] or misreported ('GEN 1@ 1x', S7).

**Why it matters:** Suppresses low-vram, fan, power-limit and PCIe false alarms; cudaMemGetInfo also under-reports.

**Next steps:**
- Use /proc/meminfo MemAvailable (+SwapFree), free -h 'available' or the DGX Dashboard (http://localhost:11000).

</details>

## Top Issues

1. No significant issues detected.

## Recommended Next Steps

1. No action; memory comes from /proc/meminfo, nvidia-smi memory fields are expected to be [N/A].
2. Use /proc/meminfo MemAvailable (+SwapFree), free -h 'available' or the DGX Dashboard (http://localhost:11000).

---

*This report was generated locally. No diagnostic data was transmitted.*  
*Redaction was applied to remove usernames, hostnames, home paths, IP addresses and serial numbers.*  
*The run command did not modify your system. Changes are made only by 'nvcheckup fix' after explicit confirmation.*  
*NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.*
