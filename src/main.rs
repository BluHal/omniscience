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
    let mut ctx: Option<i64> = None;
    let mut buf = String::new();
    if std::io::stdin().read_to_string(&mut buf).is_ok() {
        if let Ok(v) = serde_json::from_str::<serde_json::Value>(&buf) {
            if event == "pre" {
                tool = v.get("tool_name").and_then(|t| t.as_str()).unwrap_or("").to_string();
            }
            // Claude passes the conversation transcript path on every hook; the
            // latest turn's token usage tells us how much context is left.
            if let Some(p) = v.get("transcript_path").and_then(|t| t.as_str()) {
                ctx = std::fs::read_to_string(p).ok().and_then(|t| context_left_pct(&t));
            }
        }
    }
    if let Ok(conn) = db::open(&db::db_path()) {
        let _ = db::apply_event(&conn, &id, event, &tool);
        if let Some(left) = ctx {
            let _ = db::set_context_left(&conn, &id, left);
        }
    }
}

/// CONTEXT_WINDOW is the token budget "context left" is measured against. Claude's
/// default context is 200k. ponytail: a const knob — bump for 1M-context models.
const CONTEXT_WINDOW: f64 = 200_000.0;

/// context_left_pct reads a Claude transcript (JSONL) and returns the percent of
/// the context window still free, from the most recent main-chain assistant turn's
/// usage. None when there's no usage yet (a fresh session).
fn context_left_pct(transcript: &str) -> Option<i64> {
    for line in transcript.lines().rev() {
        let Ok(v) = serde_json::from_str::<serde_json::Value>(line) else { continue };
        if v.get("isSidechain").and_then(|x| x.as_bool()) == Some(true) {
            continue; // subagent turn — not the main context
        }
        let Some(u) = v.get("message").and_then(|m| m.get("usage")) else { continue };
        let tok = |k| u.get(k).and_then(|x| x.as_f64()).unwrap_or(0.0);
        let used = tok("input_tokens") + tok("cache_read_input_tokens") + tok("cache_creation_input_tokens");
        if used <= 0.0 {
            continue;
        }
        return Some((((1.0 - used / CONTEXT_WINDOW) * 100.0).round() as i64).clamp(0, 100));
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    // The token math: latest main-chain usage drives the percent, sidechain
    // (subagent) turns are ignored, and a usage-less transcript is unknown.
    #[test]
    fn context_left_from_usage() {
        let t = concat!(
            r#"{"type":"assistant","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":50000,"cache_creation_input_tokens":10000}}}"#, "\n",
            r#"{"type":"assistant","isSidechain":true,"message":{"usage":{"input_tokens":190000}}}"#, "\n",
        );
        // used = 60010 of 200000 → ~70% left; sidechain line ignored
        assert_eq!(context_left_pct(t), Some(70));
        assert_eq!(context_left_pct(r#"{"type":"user","message":{"role":"user"}}"#), None);
        assert_eq!(context_left_pct(""), None);
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
  esc esc  back to nav        ↹      move focus
  z   glance mode             ^b     broadcast        ! jump to blocked    q quit
"#
    );
}
