//! NVCheckup — Cross-platform NVIDIA Diagnostic Tool (Rust)
//! Unofficial community tool, not affiliated with NVIDIA Corporation.

mod types;
mod collector;
mod analyzer;
mod report;

use std::env;
use std::process;
use std::time::Instant;

const VERSION: &str = "0.2.0";
const DISCLAIMER: &str = "NVCheckup is an unofficial community tool, not affiliated with or endorsed by NVIDIA Corporation.";

fn main() {
    let args: Vec<String> = env::args().collect();

    if args.len() < 2 {
        print_usage();
        return;
    }

    match args[1].as_str() {
        "run" => run_cmd(&args[2..]),
        "version" | "--version" | "-v" => {
            println!("NVCheckup v{}", VERSION);
            println!("{}", DISCLAIMER);
        }
        "help" | "--help" | "-h" => print_usage(),
        other => {
            eprintln!("Unknown command: {}", other);
            print_usage();
            process::exit(3);
        }
    }
}

fn run_cmd(args: &[String]) {
    let mut mode = "full".to_string();
    let mut verbose = false;

    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "--mode" => {
                if i + 1 < args.len() {
                    mode = args[i + 1].clone();
                    i += 1;
                }
            }
            "--verbose" => verbose = true,
            _ => {}
        }
        i += 1;
    }

    // Validate mode
    match mode.as_str() {
        "gaming" | "ai" | "creator" | "streaming" | "full" => {}
        other => {
            eprintln!("Invalid mode: {}. Use: gaming, ai, creator, streaming, full", other);
            process::exit(3);
        }
    }

    println!();
    println!("  NVCheckup v{} (Rust)", VERSION);
    println!("  {}", DISCLAIMER);
    println!();

    let start = Instant::now();

    // Collect
    println!("[1/3] Collecting system and GPU information...");
    let system = collector::system::collect_system_info();
    let (gpus, driver) = collector::gpu::collect_gpu_info();

    // Analyze
    println!("[2/3] Analyzing results...");
    let rules = analyzer::rules::load_rules();
    let findings = analyzer::rules::analyze(&system, &gpus, &driver, &rules, &mode);

    // Report
    println!("[3/3] Generating report...");
    let elapsed = start.elapsed().as_secs_f64();

    let report_text = report::text::generate(
        &system, &gpus, &driver, &findings, &mode, elapsed,
    );
    println!();
    println!("{}", report_text);

    // Exit code
    let has_crit = findings.iter().any(|f| f.severity == "CRIT");
    let has_warn = findings.iter().any(|f| f.severity == "WARN");
    if has_crit {
        process::exit(2);
    } else if has_warn {
        process::exit(1);
    }
}

fn print_usage() {
    println!(
        r#"NVCheckup v{} — Cross-platform NVIDIA Diagnostic Tool (Rust)
{}

Usage:
  nvcheckup <command> [flags]

Commands:
  run         Run diagnostics and generate a report
  version     Show version information

Run Flags:
  --mode      Diagnostic mode: gaming, ai, creator, streaming, full (default: full)
  --verbose   Enable verbose output

Examples:
  nvcheckup run --mode gaming
  nvcheckup run --mode ai
  nvcheckup run --mode full
"#,
        VERSION, DISCLAIMER
    );
}
