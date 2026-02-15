//! System information collector.

use crate::types::SystemInfo;
use std::process::Command;

pub fn collect_system_info() -> SystemInfo {
    let os_name = std::env::consts::OS.to_string();
    let os_version = get_os_version();
    let arch = std::env::consts::ARCH.to_string();
    let cpu_model = get_cpu_model();
    let hostname = get_hostname();

    SystemInfo {
        os_name,
        os_version,
        architecture: arch,
        cpu_model,
        ram_total_mb: 0,
        hostname,
    }
}

fn get_os_version() -> String {
    if cfg!(target_os = "windows") {
        run_command("cmd", &["/c", "ver"])
            .unwrap_or_else(|| "unknown".to_string())
    } else {
        run_command("uname", &["-r"])
            .unwrap_or_else(|| "unknown".to_string())
    }
}

fn get_cpu_model() -> String {
    if cfg!(target_os = "windows") {
        run_command("powershell", &["-NoProfile", "-Command",
            "(Get-CimInstance Win32_Processor).Name"])
            .unwrap_or_else(|| "unknown".to_string())
    } else {
        run_command("sh", &["-c", "grep 'model name' /proc/cpuinfo | head -1 | cut -d: -f2"])
            .map(|s| s.trim().to_string())
            .unwrap_or_else(|| "unknown".to_string())
    }
}

fn get_hostname() -> String {
    if cfg!(target_os = "windows") {
        run_command("hostname", &[])
    } else {
        run_command("hostname", &[])
    }
    .unwrap_or_else(|| "unknown".to_string())
}

pub fn run_command(cmd: &str, args: &[&str]) -> Option<String> {
    Command::new(cmd)
        .args(args)
        .output()
        .ok()
        .and_then(|output| {
            if output.status.success() {
                Some(String::from_utf8_lossy(&output.stdout).trim().to_string())
            } else {
                None
            }
        })
}
