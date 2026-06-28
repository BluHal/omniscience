//! Building the command for a live Claude session: a plain (user-authed)
//! `claude --settings <file>` with omni's status hooks + the room's hcom bus
//! wired via env, plus a self-join prompt. Ported from the Go up/spawn path.

use portable_pty::CommandBuilder;
use std::path::{Path, PathBuf};

pub struct LaunchSpec {
    pub dir: PathBuf,
    pub id: String,
    pub room: String,
    pub role: String,
    pub settings: PathBuf,
    pub hcom_dir: PathBuf,
    pub brief: Option<String>,
}

/// launch_prompt prepends the hcom self-join (plain claude + hcom hooks doesn't
/// auto-join) to an agent's first instruction. A Lead (no brief) is told it can
/// delegate via `omni spawn` so parallel work shows up as separate live tiles
/// (ADR-0002: spawning routes through omni, not the native Task tool).
fn launch_prompt(spec: &LaunchSpec) -> String {
    let body = match spec.brief.as_deref() {
        Some(b) if !b.trim().is_empty() => b.trim().to_string(),
        _ => format!(
            "await the user's instructions, then work in this project. To run sub-tasks \
             in parallel as separate live agents, run:  omni spawn {} <role> [brief]  \
             (add --dir <path> to place an agent in another repo) — each appears as its \
             own tile on your team's shared bus.",
            spec.room
        ),
    };
    format!("First run: hcom start   (joins your team message bus). Then {body}")
}

pub fn claude_command(spec: &LaunchSpec) -> CommandBuilder {
    let mut cmd = CommandBuilder::new("claude");
    cmd.cwd(&spec.dir);
    for (k, v) in std::env::vars() {
        cmd.env(k, v);
    }
    cmd.env("OMNI_ID", &spec.id);
    cmd.env("OMNI_DB", crate::db::db_path().to_string_lossy().to_string());
    cmd.env("HCOM_DIR", spec.hcom_dir.to_string_lossy().to_string());
    cmd.env("HCOM_TAG", &spec.role);
    cmd.arg("--settings");
    cmd.arg(spec.settings.to_string_lossy().to_string());
    cmd.arg(launch_prompt(spec));
    cmd
}

/// hcom_dir is a room's co-located isolated bus dir under a project.
pub fn hcom_dir(project: &Path, room: &str) -> PathBuf {
    project.join(".omni").join(room).join(".hcom")
}

/// git_branch reads .git/HEAD for a quick branch label.
pub fn git_branch(dir: &Path) -> String {
    match std::fs::read_to_string(dir.join(".git").join("HEAD")) {
        Ok(s) => s.trim().rsplit('/').next().unwrap_or("main").to_string(),
        Err(_) => "—".to_string(),
    }
}
