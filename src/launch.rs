//! Building the command for a live Claude session. For now a plain `claude` in
//! the project dir; status hooks + hcom wiring are layered on next (port of the
//! Go up/spawn launch path).

use portable_pty::CommandBuilder;
use std::path::Path;

pub fn claude_command(dir: &Path) -> CommandBuilder {
    let mut cmd = CommandBuilder::new("claude");
    cmd.cwd(dir);
    // inherit the user's environment so auth/theme are the real ones
    for (k, v) in std::env::vars() {
        cmd.env(k, v);
    }
    cmd
}

/// project_roots are the dirs the picker scans one level deep.
pub fn project_roots() -> Vec<std::path::PathBuf> {
    let home = dirs::home_dir().unwrap_or_default();
    ["work", "src", "dev"].iter().map(|s| home.join(s)).collect()
}

/// git_branch reads .git/HEAD for a quick branch label.
pub fn git_branch(dir: &Path) -> String {
    match std::fs::read_to_string(dir.join(".git/HEAD")) {
        Ok(s) => s
            .trim()
            .rsplit('/')
            .next()
            .unwrap_or("main")
            .to_string(),
        Err(_) => "—".to_string(),
    }
}
