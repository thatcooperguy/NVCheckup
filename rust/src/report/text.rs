//! Text report generator.

use crate::types::{SystemInfo, GPUInfo, DriverInfo, Finding};

const DISCLAIMER: &str = "NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.";

pub fn generate(
    system: &SystemInfo,
    gpus: &[GPUInfo],
    driver: &DriverInfo,
    findings: &[Finding],
    mode: &str,
    runtime_secs: f64,
) -> String {
    let mut out = String::new();
    let line = "─".repeat(72);

    out.push_str(&line);
    out.push('\n');
    out.push_str(&format!("  NVCheckup v0.2.0 — NVIDIA Diagnostic Report (Rust)\n"));
    out.push_str(&format!("  {}\n", DISCLAIMER));
    out.push_str(&line);
    out.push('\n');
    out.push_str(&format!("  Mode:      {}\n", mode));
    out.push_str(&format!("  Platform:  {}\n", system.os_name));
    out.push_str(&format!("  Runtime:   {:.1}s\n", runtime_secs));
    out.push_str(&line);
    out.push('\n');

    // System
    out.push_str("\n== SYSTEM INFO ==\n\n");
    out.push_str(&format!("  OS:           {} {}\n", system.os_name, system.os_version));
    out.push_str(&format!("  Architecture: {}\n", system.architecture));
    out.push_str(&format!("  CPU:          {}\n", system.cpu_model));
    out.push_str(&line);
    out.push('\n');

    // GPUs
    out.push_str("\n== GPU INVENTORY ==\n\n");
    if gpus.is_empty() {
        out.push_str("  No GPUs detected.\n");
    } else {
        for gpu in gpus {
            out.push_str(&format!("  [GPU {}] {}\n", gpu.index, gpu.name));
            out.push_str(&format!("    Driver:  {}\n", gpu.driver_version));
            if gpu.vram_total_mb > 0 {
                out.push_str(&format!("    VRAM:    {} MB\n", gpu.vram_total_mb));
            }
            if gpu.temperature_c > 0 {
                out.push_str(&format!("    Temp:    {}°C\n", gpu.temperature_c));
            }
            out.push('\n');
        }
    }
    out.push_str(&format!("  NVIDIA Driver: {}\n", if driver.version.is_empty() { "N/A" } else { &driver.version }));
    out.push_str(&format!("  CUDA (driver): {}\n", if driver.cuda_version.is_empty() { "N/A" } else { &driver.cuda_version }));
    out.push_str(&line);
    out.push('\n');

    // Findings
    out.push_str("\n== FINDINGS ==\n\n");
    if findings.is_empty() {
        out.push_str("  No issues detected.\n");
    } else {
        let crit = findings.iter().filter(|f| f.severity == "CRIT").count();
        let warn = findings.iter().filter(|f| f.severity == "WARN").count();
        let info = findings.iter().filter(|f| f.severity == "INFO").count();
        out.push_str(&format!("  Total: {} CRITICAL, {} WARNING, {} INFO\n\n", crit, warn, info));

        for (i, f) in findings.iter().enumerate() {
            out.push_str(&format!("  [{}] #{}: {} (confidence: {}%)\n", f.severity, i + 1, f.title, f.confidence));
            out.push_str(&format!("    Evidence:     {}\n", f.evidence));
            out.push_str(&format!("    Why:          {}\n", f.why_it_matters));
            out.push('\n');
        }
    }
    out.push_str(&line);
    out.push('\n');

    // Privacy
    out.push_str("\n== PRIVACY & DATA ==\n\n");
    out.push_str("  This report was generated locally. No data was sent anywhere.\n");
    out.push_str("  NVCheckup does not modify your system, drivers, or settings.\n\n");
    out.push_str(&line);
    out.push('\n');
    out.push_str(&format!("  {}\n", DISCLAIMER));
    out.push_str(&line);
    out.push('\n');

    out
}
