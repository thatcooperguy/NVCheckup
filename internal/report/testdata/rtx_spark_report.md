# NVCheckup Diagnostic Report

> NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.

**Generated:** 2026-09-02 10:00:00 | **Mode:** full | **Platform:** windows | **Runtime:** 3.1s

## Summary

```
NVCheckup v0.2.2 | 2026-09-02 10:00:00
OS: Microsoft Windows 11 Pro 24H2 | Arch: arm64
Platform: RTX Spark (Microsoft Surface RTX Spark Dev Box)
GPU: NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU) | Driver: 616.00
Unified memory: 128.0 GiB total, 100.0 GiB available
Findings: 0 CRITICAL, 1 WARNING, 4 total
Top: RTX Spark Developer Preview Driver
```

## System

| Property | Value |
|----------|-------|
| OS | Microsoft Windows 11 Pro 24H2 (Build 26100) |
| Architecture | arm64 |
| CPU | NVIDIA N1X (20 cores) |
| RAM | 131072 MB |
| Boot Mode | UEFI |
| Secure Boot | Enabled |
| Uptime | 0d 4h 12m |

## Platform

| Property | Value |
|----------|-------|
| Class | RTX Spark (rtx-spark) |
| Vendor/Model | Microsoft Surface RTX Spark Dev Box |
| GPU SoC | N1X (compute capability 12.1) |
| Unified memory | yes (nvidia-smi memory, fan, power limit and PCIe fields are [N/A] by design) |
| Memory pool | 128.0 GiB total, 100.0 GiB available, 100.0 GiB allocatable (MemAvailable + SwapFree; HugePages override, spec 3.3) |
| Windows on Arm | yes (native ARM64, NVCheckup emulated: no) |

## Unified Memory

| Property | Value |
|----------|-------|
| MemTotal | 128.0 GiB |
| MemAvailable | 100.0 GiB |
| MemFree | 100.0 GiB |
| Buffers + Cached | 0.0 GiB (reclaimable) |
| Swap | 0.0 of 0.0 GiB used (N/A) |
| Swappiness | 0 |
| Allocatable | 100.0 GiB |
| PSI memory | some avg10 0.00, full avg10 0.00 |
| GPU processes | 0 |
| OOM events | 0 OOM-killer, 0 NVRM no-memory |

## GPUs

### GPU 0: NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)

| Property | Value |
|----------|-------|
| Vendor | NVIDIA |
| Driver | 616.00 |
| Memory | unified pool (nvidia-smi reports [N/A]; see Platform) |
| Compute capability | 12.1 |
| Package | on-package GPU (NVLink-C2C to the CPU) |

**NVIDIA Driver:** 616.00 | **CUDA (driver):** N/A

## Windows

| Property | Value |
|----------|-------|
| HAGS | Default (not configured) |
| Game Mode | Enabled |
| Power Plan | High performance |
| Driver resets (4101) | 0 in last 30 days |
| nvlddmkm errors | 0 in last 30 days |
| WHEA errors | 0 in last 30 days |

## Findings

| Severity | Finding | Evidence | Next Step |
|----------|---------|----------|-----------|
| **WARN** (impact: persistent) | RTX Spark Developer Preview Driver | Driver 616.00 (nv_surface_woa.inf) is the RTX Spark Developer Preview branch ... | Check the RTX Spark Developer Preview thread (S24) and OEM/Windows Update for... |
| **INFO** | Unified Memory: nvidia-smi Memory Fields Are [N/A] by Design | nvidia-smi memory '[N/A]' on GPU 0 (NVIDIA RTX Spark N1X (6144-core Blackwell... | Use /proc/meminfo MemAvailable (+SwapFree), free -h 'available' or the DGX Da... |
| **INFO** | NVIDIA RTX Spark (N1X) Detected | RTX Spark N1X (6144-core, DEV_2E03), Windows build 26100 ARM64, Microsoft Sur... | Report total RAM as the GPU pool; record driver version and INF. |
| **INFO** | nvidia-smi Not Found (may be absent on RTX Spark) | The nvidia-smi utility was not found. Whether the RTX Spark Arm64 driver pack... | No action; if a later RTX Spark driver adds nvidia-smi.exe, re-run NVCheckup ... |

### Details

<details>
<summary><b>[WARN] (impact: persistent) #1: RTX Spark Developer Preview Driver</b></summary>

**ID:** `rtx-spark-driver-developer-preview`

**Evidence:** Driver 616.00 (nv_surface_woa.inf) is the RTX Spark Developer Preview branch (R616, 2026-07-16, S24); below 616.00 on Arm64 is an extracted/unofficial build.

**Why it matters:** NVIDIA lists the preview stack as pre-release, not for production or benchmarking (S26 release notes), with the known issue 'Possible system instability during PyTorch build workflows' (S24 landing page). (The v1 '~2 min blank display during install' claim had no source and was dropped.)

**Next steps:**
- Check the RTX Spark Developer Preview thread (S24) and OEM/Windows Update for a production Arm64 driver (read-only).
- **Advisory:** installing a different driver replaces the Developer Preview package (revert: reinstall the 616.00 DP package from the S24 thread).

</details>

<details>
<summary><b>[INFO] #2: Unified Memory: nvidia-smi Memory Fields Are [N/A] by Design</b></summary>

**ID:** `unified-memory-nvsmi-expected`

**Evidence:** nvidia-smi memory '[N/A]' on GPU 0 (NVIDIA RTX Spark N1X (6144-core Blackwell RTX GPU)) is expected on unified-memory iGPUs. Pool: MemTotal 128.0 GiB measured from /proc/meminfo. MemAvailable 100.0 GiB, swap 0.0 GiB. Fan, power limit, memory clock and PCIe gen/width are also [N/A] or misreported ('GEN 1@ 1x', S7).

**Why it matters:** Suppresses low-vram, fan, power-limit and PCIe false alarms; cudaMemGetInfo also under-reports.

**Next steps:**
- Use /proc/meminfo MemAvailable (+SwapFree), free -h 'available' or the DGX Dashboard (http://localhost:11000).

</details>

<details>
<summary><b>[INFO] #3: NVIDIA RTX Spark (N1X) Detected</b></summary>

**ID:** `rtx-spark-detected`

**Evidence:** RTX Spark N1X (6144-core, DEV_2E03), Windows build 26100 ARM64, Microsoft Surface RTX Spark Dev Box, pool 128.0 GiB (Win32_OperatingSystem.TotalVisibleMemorySize).

**Why it matters:** Unified memory and Windows-on-Arm change what nvidia-smi, AdapterRAM (uint32) and CUDA report.

**Next steps:**
- Report total RAM as the GPU pool; record driver version and INF.

</details>

<details>
<summary><b>[INFO] #4: nvidia-smi Not Found (may be absent on RTX Spark)</b></summary>

**ID:** `nvidia-smi-missing`

**Evidence:** The nvidia-smi utility was not found. Whether the RTX Spark Arm64 driver package ships nvidia-smi.exe is unconfirmed (spec 2.2), so this is informational.

**Why it matters:** Without nvidia-smi the GPU, thermal and PCIe samples come from WMI only; memory is the unified pool (Win32_OperatingSystem.TotalVisibleMemorySize), not AdapterRAM.

**Next steps:**
- No action; if a later RTX Spark driver adds nvidia-smi.exe, re-run NVCheckup for the fuller sample set.

</details>

## Top Issues

1. [WARN] RTX Spark Developer Preview Driver (80% confidence)

## Recommended Next Steps

1. Check the RTX Spark Developer Preview thread (S24) and OEM/Windows Update for a production Arm64 driver (read-only).
2. Advisory: installing a different driver replaces the Developer Preview package (revert: reinstall the 616.00 DP package from the S24 thread).

---

*This report was generated locally. No diagnostic data was transmitted.*  
*Redaction was applied to remove usernames, hostnames, home paths and IP addresses.*  
*The run command did not modify your system. Changes are made only by 'nvcheckup fix' after explicit confirmation.*  
*NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.*
