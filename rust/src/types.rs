//! Shared data types mirroring the Go types package.

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SystemInfo {
    pub os_name: String,
    pub os_version: String,
    pub architecture: String,
    pub cpu_model: String,
    pub ram_total_mb: i64,
    pub hostname: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GPUInfo {
    pub index: usize,
    pub name: String,
    pub vendor: String,
    pub driver_version: String,
    pub vram_total_mb: i64,
    pub temperature_c: i32,
    pub is_nvidia: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DriverInfo {
    pub version: String,
    pub cuda_version: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Finding {
    pub severity: String,
    pub title: String,
    pub evidence: String,
    pub why_it_matters: String,
    pub next_steps: Vec<String>,
    pub confidence: u32,
    pub category: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Rule {
    pub id: String,
    pub title: String,
    pub category: String,
    pub severity: String,
    #[serde(default)]
    pub base_confidence: u32,
    pub modes: Vec<String>,
    #[serde(default)]
    pub platform: Option<String>,
    pub description: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RulesFile {
    pub description: String,
    pub version: String,
    pub rules: Vec<Rule>,
}
