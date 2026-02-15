//! Rule engine that loads diagnostic rules from the shared knowledge pack.

use crate::types::{Finding, Rule, RulesFile, SystemInfo, GPUInfo, DriverInfo};

/// Embedded knowledge pack rules.
const RULES_JSON: &str = include_str!("../../../knowledge/rules.json");

/// Load diagnostic rules from the embedded knowledge pack.
pub fn load_rules() -> Vec<Rule> {
    let rules_file: RulesFile = serde_json::from_str(RULES_JSON)
        .expect("Failed to parse embedded rules.json");
    rules_file.rules
}

/// Analyze collected data against loaded rules for the given mode.
pub fn analyze(
    system: &SystemInfo,
    gpus: &[GPUInfo],
    driver: &DriverInfo,
    rules: &[Rule],
    mode: &str,
) -> Vec<Finding> {
    let mut findings = Vec::new();

    for rule in rules {
        // Skip rules not applicable to this mode
        if !rule.modes.contains(&mode.to_string()) {
            continue;
        }

        // Skip platform-specific rules on wrong platform
        if let Some(ref platform) = rule.platform {
            let current = std::env::consts::OS;
            if platform != current {
                continue;
            }
        }

        // Check each rule
        if let Some(finding) = evaluate_rule(rule, system, gpus, driver) {
            findings.push(finding);
        }
    }

    // Sort by severity: CRIT first, then WARN, then INFO
    findings.sort_by(|a, b| severity_order(&a.severity).cmp(&severity_order(&b.severity)));

    findings
}

fn severity_order(s: &str) -> u8 {
    match s {
        "CRIT" => 0,
        "WARN" => 1,
        "INFO" => 2,
        _ => 3,
    }
}

fn evaluate_rule(
    rule: &Rule,
    _system: &SystemInfo,
    gpus: &[GPUInfo],
    driver: &DriverInfo,
) -> Option<Finding> {
    match rule.id.as_str() {
        "no-nvidia-gpu" => {
            let has_nvidia = gpus.iter().any(|g| g.is_nvidia);
            if !has_nvidia && gpus.is_empty() {
                return Some(make_finding(rule, "No NVIDIA GPU detected in system."));
            }
            None
        }
        "hybrid-gpu" => {
            let nvidia_count = gpus.iter().filter(|g| g.is_nvidia).count();
            let total = gpus.len();
            if nvidia_count > 0 && total > nvidia_count {
                return Some(make_finding(rule, "Both NVIDIA and integrated graphics detected."));
            }
            None
        }
        "driver-not-detected" => {
            if driver.version.is_empty() {
                return Some(make_finding(rule, "nvidia-smi did not return a driver version."));
            }
            None
        }
        "nvidia-smi-missing" => {
            // If we got no GPUs and no driver, nvidia-smi is probably missing
            if gpus.is_empty() && driver.version.is_empty() {
                return Some(make_finding(rule, "nvidia-smi was not found or returned no data."));
            }
            None
        }
        "low-vram" => {
            for gpu in gpus {
                if gpu.is_nvidia && gpu.vram_total_mb > 0 && gpu.vram_total_mb < 4096 {
                    return Some(make_finding(
                        rule,
                        &format!("GPU {} has {} MB VRAM (< 4 GB).", gpu.name, gpu.vram_total_mb),
                    ));
                }
            }
            None
        }
        "gpu-running-hot" => {
            for gpu in gpus {
                if gpu.temperature_c >= 75 && gpu.temperature_c < 85 {
                    return Some(make_finding(
                        rule,
                        &format!("GPU temperature is {}°C.", gpu.temperature_c),
                    ));
                }
            }
            None
        }
        "thermal-throttling" => {
            for gpu in gpus {
                if gpu.temperature_c >= 85 {
                    return Some(make_finding(
                        rule,
                        &format!("GPU temperature is {}°C — exceeds safe limit.", gpu.temperature_c),
                    ));
                }
            }
            None
        }
        _ => None, // Unimplemented rules are skipped
    }
}

fn make_finding(rule: &Rule, evidence: &str) -> Finding {
    Finding {
        severity: rule.severity.clone(),
        title: rule.title.clone(),
        evidence: evidence.to_string(),
        why_it_matters: rule.description.clone(),
        next_steps: vec![], // TODO: Load from remediations.json
        confidence: rule.base_confidence,
        category: rule.category.clone(),
    }
}
