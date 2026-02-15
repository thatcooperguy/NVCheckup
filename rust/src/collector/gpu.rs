//! GPU and driver information collector via nvidia-smi.

use crate::types::{GPUInfo, DriverInfo};
use crate::collector::system::run_command;

pub fn collect_gpu_info() -> (Vec<GPUInfo>, DriverInfo) {
    let mut gpus = Vec::new();
    let mut driver = DriverInfo {
        version: String::new(),
        cuda_version: String::new(),
    };

    // Try nvidia-smi for GPU info
    let gpu_output = run_command(
        "nvidia-smi",
        &["--query-gpu=index,name,driver_version,memory.total,temperature.gpu",
          "--format=csv,noheader,nounits"],
    );

    if let Some(output) = gpu_output {
        for line in output.lines() {
            let fields: Vec<&str> = line.split(", ").collect();
            if fields.len() >= 5 {
                let index = fields[0].trim().parse::<usize>().unwrap_or(0);
                let name = fields[1].trim().to_string();
                let driver_ver = fields[2].trim().to_string();
                let vram = fields[3].trim().parse::<i64>().unwrap_or(0);
                let temp = fields[4].trim().parse::<i32>().unwrap_or(0);

                if driver.version.is_empty() {
                    driver.version = driver_ver.clone();
                }

                gpus.push(GPUInfo {
                    index,
                    name,
                    vendor: "NVIDIA".to_string(),
                    driver_version: driver_ver,
                    vram_total_mb: vram,
                    temperature_c: temp,
                    is_nvidia: true,
                });
            }
        }
    }

    // Query CUDA version
    let cuda_output = run_command(
        "nvidia-smi",
        &["--query-gpu=driver_version", "--format=csv,noheader"],
    );
    // Full nvidia-smi output usually has CUDA version in header
    let smi_output = run_command("nvidia-smi", &[]);
    if let Some(output) = smi_output {
        // Look for "CUDA Version: 12.x"
        for line in output.lines() {
            if line.contains("CUDA Version") {
                if let Some(pos) = line.find("CUDA Version:") {
                    let rest = &line[pos + 14..];
                    let ver: String = rest.chars()
                        .take_while(|c| c.is_ascii_digit() || *c == '.')
                        .collect();
                    if !ver.is_empty() {
                        driver.cuda_version = ver.trim().to_string();
                    }
                }
            }
        }
    }

    (gpus, driver)
}
