// Some theme glyphs/colors and helpers are defined ahead of the screens that use
// them — don't warn on the not-yet-wired ones.
#![allow(dead_code)]

mod app;
mod db;
mod hooks;
mod launch;
mod recents;
mod term;
mod theme;
mod ui;

use anyhow::Result;
use std::io::{IsTerminal, Read};

fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    match args.get(1).map(|s| s.as_str()) {
        Some("hook") => {
            run_hook(args.get(2).map(|s| s.as_str()).unwrap_or(""));
            return Ok(());
        }
        Some("spawn") => return run_spawn(&args[2..]),
        Some("-h") | Some("--help") => {
            print_help();
            return Ok(());
        }
        _ => {}
    }
    if !std::io::stdout().is_terminal() {
        eprintln!("omni: needs an interactive terminal (run it directly, not piped).");
        std::process::exit(1);
    }
    let mut terminal = ratatui::init();
    let res = app::run_loop(&mut terminal);
    ratatui::restore();
    res
}

/// run_hook is invoked by Claude Code as `omni hook <event>`. Identity arrives
/// via OMNI_ID/OMNI_DB env the launching process exported. It must never break
/// the agent, so it always returns quietly.
fn run_hook(event: &str) {
    let id = std::env::var("OMNI_ID").unwrap_or_default();
    if id.is_empty() || event.is_empty() {
        return;
    }
    let mut tool = String::new();
    if event == "pre" {
        let mut buf = String::new();
        if std::io::stdin().read_to_string(&mut buf).is_ok() {
            if let Ok(v) = serde_json::from_str::<serde_json::Value>(&buf) {
                tool = v.get("tool_name").and_then(|t| t.as_str()).unwrap_or("").to_string();
            }
        }
    }
    if let Ok(conn) = db::open(&db::db_path()) {
        let _ = db::apply_event(&conn, &id, event, &tool);
    }
}

/// run_spawn queues a request the running dashboard picks up (a Lead shells out
/// to this to add an agent to its group).
fn run_spawn(args: &[String]) -> Result<()> {
    // optional --dir <path> places the agent in another repo (cross-repo group)
    let mut dir: Option<String> = None;
    let mut pos: Vec<String> = Vec::new();
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--dir" | "-C" => dir = it.next().cloned(),
            _ => pos.push(a.clone()),
        }
    }
    if pos.len() < 2 {
        eprintln!("usage: omni spawn [--dir <path>] <room> <role> [brief]");
        std::process::exit(2);
    }
    let (room, role) = (&pos[0], &pos[1]);
    let brief = pos.get(2..).map(|s| s.join(" ")).unwrap_or_default();
    let project = match dir {
        Some(d) => std::fs::canonicalize(&d).unwrap_or_else(|_| std::path::PathBuf::from(d)).to_string_lossy().into_owned(),
        None => std::env::current_dir()?.to_string_lossy().into_owned(),
    };
    let conn = db::open(&db::db_path())?;
    db::enqueue_spawn(&conn, room, role, &project, &brief)?;
    println!("spawn queued: {room}/{role} @ {project}");
    Ok(())
}

fn print_help() {
    print!(
        r#"omni — terminal dashboard for live Claude Code sessions

  omni                     open the dashboard (run it in a real terminal)
  omni spawn <room> <role> [brief]   add an agent to a live group
  omni hook  <event>       (internal: Claude Code status hook)

  ^n  new project (picker)    i / ⏎  type into focused tile
  ^\  back to nav             ↹      move focus
  z   glance mode             ^b     broadcast        ! jump to blocked    q quit
"#
    );
}
