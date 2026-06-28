// Some theme glyphs/colors and helpers are defined ahead of the screens that use
// them (status feed, hcom chat land next) — don't warn on the not-yet-wired ones.
#![allow(dead_code)]

mod app;
mod launch;
mod term;
mod theme;
mod ui;

use anyhow::Result;
use std::io::IsTerminal;

fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().collect();
    if args.iter().any(|a| a == "-h" || a == "--help") {
        print!(
            r#"omni — terminal dashboard for live Claude Code sessions

  Run `omni` in a terminal to open the dashboard.

  ^n  new project (picker)    i / ⏎  type into focused tile
  ^\  back to nav             ↹      move focus
  z   glance mode             !      jump to blocked     q  quit
"#
        );
        return Ok(());
    }
    if !std::io::stdout().is_terminal() {
        eprintln!("omni: needs an interactive terminal (run it directly, not piped).");
        std::process::exit(1);
    }
    // omni → the dashboard. (omni up/spawn/hook + state.db status + hcom land next.)
    let mut terminal = ratatui::init();
    let res = app::run_loop(&mut terminal);
    ratatui::restore();
    res
}
