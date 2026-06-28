//! A tiny most-recently-opened-projects list (newline-separated abs paths under
//! ~/.omni/recents) so the empty screen can "resume last" and the picker can
//! surface recents.

use std::path::{Path, PathBuf};

fn file() -> PathBuf {
    dirs::home_dir().unwrap_or_default().join(".omni").join("recents")
}

pub fn list() -> Vec<PathBuf> {
    std::fs::read_to_string(file())
        .unwrap_or_default()
        .lines()
        .filter(|l| !l.trim().is_empty())
        .map(PathBuf::from)
        .collect()
}

pub fn push(path: &Path) {
    let mut list = list();
    list.retain(|p| p != path);
    list.insert(0, path.to_path_buf());
    list.truncate(20);
    let f = file();
    if let Some(d) = f.parent() {
        let _ = std::fs::create_dir_all(d);
    }
    let joined = list.iter().map(|p| p.to_string_lossy().into_owned()).collect::<Vec<_>>().join("\n");
    let _ = std::fs::write(f, joined);
}

/// display relativizes a path against $HOME for the UI (~/work/acme-web).
pub fn display(path: &Path) -> String {
    let home = dirs::home_dir().unwrap_or_default().to_string_lossy().into_owned();
    path.to_string_lossy().replacen(&home, "~", 1)
}
