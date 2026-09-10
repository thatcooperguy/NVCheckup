# Windows host memory alongside a game and local AI

A model fitting in GPU memory does not establish that the Windows host is ready
to run it alongside a game, editor or compiler. Windows can charge committed
memory before those pages occupy physical RAM. A large idle AI service can
therefore leave free RAM visible while reducing the capacity for new allocations.

NVCheckup records a separate host snapshot in `system.windows_memory` in a
diagnostic report and `host_windows_memory` in a Windows LLM plan. Text and
Markdown display committed bytes, the commit limit, their difference (headroom),
physical RAM free, the source and capture time. The collector uses one bounded,
read-only PowerShell invocation with two CIM reads:

- `Win32_OperatingSystem.TotalVisibleMemorySize` and `FreePhysicalMemory`, in kB.
- `Win32_PerfFormattedData_PerfOS_Memory.CommitLimit` and `CommittedBytes`, in bytes.

Headroom is `CommitLimit - CommittedBytes`. The operating system's
`FreeVirtualMemory` field is not substituted for these counters. The commit
limit is a snapshot and may change; this check does not predict future pagefile
growth or the amount required by a particular build. Microsoft documents the
[commit counters](https://learn.microsoft.com/en-us/windows/win32/memory/memory-performance-information)
and [why committing memory can precede occupying physical RAM](https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-performance_information).

The warning threshold is a heuristic: headroom below **4 GiB or 10% of the
recorded limit**. A warning recommends reviewing Task Manager's **Performance >
Memory > Committed**, unloading unneeded idle AI models/services, or reducing
compiler parallelism to 1–2 jobs before rechecking. It does not stop services,
change a pagefile, change global build settings, or download anything.

Commit capacity is not a fast-memory pool. It never increases GPU VRAM, the model
fit estimate, or the Windows physical-memory fallback. Linux/Spark memory rules
are unchanged. A plan can still show that a model fits its GPU pool while an
adjacent host warning says that GPU fit does not establish host readiness.

Missing counters are unknown. A reported zero committed value or zero remaining
headroom stays known; a zero limit, a missing member or committed bytes above the
limit makes headroom unknown. Old reports with no snapshot say that the data
was not recorded. If performance counters are unavailable, physical RAM can
still be reported without inventing commit capacity.

Planning from `--report` uses only the saved snapshot; it never refreshes from
the PC reading that file. Snapshot time is preserved, and warnings refer to that
recorded state. Capture a new report to reassess current conditions.

Portable tests cover low commit with free RAM, missing/zero/inconsistent data,
saved-report isolation, unchanged GPU sizing and Linux/Spark behavior. An
optional live Windows check runs the actual collector:

```powershell
$env:NVC_TEST_LIVE_WINDOWS_MEMORY = '1'
go test ./internal/collector/common -run '^TestCollectWindowsHostMemoryLive$' -v -count=1
Remove-Item Env:NVC_TEST_LIVE_WINDOWS_MEMORY
```

That opt-in test is skipped in normal CI. It reports the state at capture time;
it cannot reconstruct the memory state of an earlier failure.
