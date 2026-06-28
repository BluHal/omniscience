//! state.db — the status of every spawned session, written by omni's own Claude
//! hooks and read by the dashboard. Also a tiny spawn-request queue so a Lead
//! can shell out to `omni spawn` and have the running TUI pick it up.

use anyhow::Result;
use rusqlite::Connection;
use std::collections::HashMap;
use std::path::PathBuf;
use std::time::{SystemTime, UNIX_EPOCH};

pub fn db_path() -> PathBuf {
    if let Ok(p) = std::env::var("OMNI_DB") {
        return PathBuf::from(p);
    }
    dirs::home_dir().unwrap_or_default().join(".omni").join("state.db")
}

pub fn open(path: &std::path::Path) -> Result<Connection> {
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)?;
    }
    let conn = Connection::open(path)?;
    conn.pragma_update(None, "journal_mode", "WAL")?;
    conn.pragma_update(None, "busy_timeout", 5000)?;
    conn.execute(
        "CREATE TABLE IF NOT EXISTS sessions(
            id TEXT PRIMARY KEY, room TEXT, project_path TEXT, role TEXT,
            status TEXT, current_activity TEXT, started_at INTEGER, last_event_at INTEGER)",
        [],
    )?;
    conn.execute(
        "CREATE TABLE IF NOT EXISTS spawn_requests(
            id INTEGER PRIMARY KEY AUTOINCREMENT, room TEXT, role TEXT,
            project_path TEXT, brief TEXT, handled INTEGER DEFAULT 0)",
        [],
    )?;
    Ok(conn)
}

pub fn now() -> i64 {
    SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs() as i64
}

pub fn insert_session(conn: &Connection, id: &str, room: &str, project: &str, role: &str) -> Result<()> {
    let t = now();
    conn.execute(
        "INSERT OR REPLACE INTO sessions
            (id,room,project_path,role,status,current_activity,started_at,last_event_at)
            VALUES(?1,?2,?3,?4,'starting','',?5,?5)",
        rusqlite::params![id, room, project, role, t],
    )?;
    Ok(())
}

/// apply_event maps a Claude hook event to a status/activity update on one row.
/// Unit-tested so the hook → db logic is verifiable without spawning Claude.
pub fn apply_event(conn: &Connection, id: &str, event: &str, tool: &str) -> Result<()> {
    let t = now();
    match event {
        "sessionstart" => {
            conn.execute("UPDATE sessions SET status='working', last_event_at=?2 WHERE id=?1", rusqlite::params![id, t])?;
        }
        "pre" => {
            conn.execute("UPDATE sessions SET status='working', current_activity=?2, last_event_at=?3 WHERE id=?1", rusqlite::params![id, tool, t])?;
        }
        "notify" => {
            conn.execute("UPDATE sessions SET status='blocked', last_event_at=?2 WHERE id=?1", rusqlite::params![id, t])?;
        }
        "stop" => {
            conn.execute("UPDATE sessions SET status='idle', current_activity='', last_event_at=?2 WHERE id=?1", rusqlite::params![id, t])?;
        }
        "end" => {
            conn.execute("UPDATE sessions SET status='done', last_event_at=?2 WHERE id=?1", rusqlite::params![id, t])?;
        }
        _ => {}
    }
    Ok(())
}

#[derive(Clone, Default)]
pub struct Status {
    pub status: String,
    pub activity: String,
}

/// A session to restore on the next omni start: enough to re-launch the agent
/// in the same project/room/role (Claude resumes its own conversation via -c).
pub struct SavedSession {
    pub room: String,
    pub project: String,
    pub role: String,
}

/// load_sessions returns the open tiles to restore, oldest first. Ended ('done')
/// rows are skipped so a session the user closed isn't reopened.
pub fn load_sessions(conn: &Connection) -> Result<Vec<SavedSession>> {
    let mut stmt = conn
        .prepare("SELECT room, project_path, role FROM sessions WHERE status != 'done' ORDER BY started_at, rowid")?;
    let rows = stmt.query_map([], |r| {
        Ok(SavedSession { room: r.get(0)?, project: r.get(1)?, role: r.get(2)? })
    })?;
    let mut out = Vec::new();
    for row in rows {
        out.push(row?);
    }
    Ok(out)
}

pub fn clear_sessions(conn: &Connection) -> Result<()> {
    conn.execute("DELETE FROM sessions", [])?;
    Ok(())
}

pub fn delete_session(conn: &Connection, id: &str) -> Result<()> {
    conn.execute("DELETE FROM sessions WHERE id=?1", rusqlite::params![id])?;
    Ok(())
}

/// statuses returns id → (status, activity) for every known session.
pub fn statuses(conn: &Connection) -> Result<HashMap<String, Status>> {
    let mut stmt = conn.prepare("SELECT id, status, current_activity FROM sessions")?;
    let rows = stmt.query_map([], |r| {
        Ok((
            r.get::<_, String>(0)?,
            Status { status: r.get::<_, String>(1)?, activity: r.get::<_, String>(2)? },
        ))
    })?;
    let mut out = HashMap::new();
    for row in rows {
        let (id, s) = row?;
        out.insert(id, s);
    }
    Ok(out)
}

// ---- spawn request queue (omni spawn → running TUI) ----

pub struct SpawnReq {
    pub id: i64,
    pub room: String,
    pub role: String,
    pub project_path: String,
    pub brief: String,
}

pub fn enqueue_spawn(conn: &Connection, room: &str, role: &str, project: &str, brief: &str) -> Result<()> {
    conn.execute(
        "INSERT INTO spawn_requests(room,role,project_path,brief) VALUES(?1,?2,?3,?4)",
        rusqlite::params![room, role, project, brief],
    )?;
    Ok(())
}

pub fn take_spawn_requests(conn: &Connection) -> Result<Vec<SpawnReq>> {
    let mut out = Vec::new();
    {
        let mut stmt = conn.prepare("SELECT id,room,role,project_path,brief FROM spawn_requests WHERE handled=0 ORDER BY id")?;
        let rows = stmt.query_map([], |r| {
            Ok(SpawnReq {
                id: r.get(0)?,
                room: r.get(1)?,
                role: r.get(2)?,
                project_path: r.get(3)?,
                brief: r.get(4)?,
            })
        })?;
        for row in rows {
            out.push(row?);
        }
    }
    for req in &out {
        conn.execute("UPDATE spawn_requests SET handled=1 WHERE id=?1", rusqlite::params![req.id])?;
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    // The runnable check behind the hook→db status logic: each event lands the
    // right status/activity on the row, without spawning Claude.
    #[test]
    fn event_transitions() {
        let conn = Connection::open_in_memory().unwrap();
        conn.execute(
            "CREATE TABLE sessions(id TEXT PRIMARY KEY, room TEXT, project_path TEXT, role TEXT,
                status TEXT, current_activity TEXT, started_at INTEGER, last_event_at INTEGER)",
            [],
        )
        .unwrap();
        insert_session(&conn, "x", "room", "/p", "lead").unwrap();
        let cases = [
            ("sessionstart", "", "working", ""),
            ("pre", "Bash", "working", "Bash"),
            ("notify", "", "blocked", "Bash"), // activity persists while blocked
            ("stop", "", "idle", ""),          // idle clears activity
            ("end", "", "done", ""),
        ];
        for (ev, tool, want_s, want_a) in cases {
            apply_event(&conn, "x", ev, tool).unwrap();
            let (s, a): (String, String) = conn
                .query_row("SELECT status,current_activity FROM sessions WHERE id='x'", [], |r| {
                    Ok((r.get(0)?, r.get(1)?))
                })
                .unwrap();
            assert_eq!(s, want_s, "event {ev} status");
            assert_eq!(a, want_a, "event {ev} activity");
        }
    }

    // The restore set must track the open tiles: load skips ended ('done')
    // sessions, delete forgets one tile, clear empties the table.
    #[test]
    fn restore_set_tracks_open_tiles() {
        let conn = open(std::path::Path::new(":memory:")).unwrap();
        insert_session(&conn, "a", "r1", "/p1", "lead").unwrap();
        insert_session(&conn, "b", "r2", "/p2", "dev").unwrap();
        apply_event(&conn, "b", "end", "").unwrap(); // b ended → status 'done'
        let open: Vec<String> = load_sessions(&conn).unwrap().into_iter().map(|s| s.room).collect();
        assert_eq!(open, vec!["r1"], "ended session is not restored");
        delete_session(&conn, "a").unwrap();
        assert!(load_sessions(&conn).unwrap().is_empty(), "deleted tile forgotten");
        insert_session(&conn, "c", "r3", "/p3", "lead").unwrap();
        clear_sessions(&conn).unwrap();
        assert_eq!(conn.query_row("SELECT COUNT(*) FROM sessions", [], |r| r.get::<_, i64>(0)).unwrap(), 0);
    }
}

/// random_id is a short unique id for a session (OMNI_ID).
pub fn random_id() -> String {
    use std::sync::atomic::{AtomicU64, Ordering};
    static CTR: AtomicU64 = AtomicU64::new(0);
    let n = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_nanos() as u64
        ^ (CTR.fetch_add(1, Ordering::Relaxed).wrapping_mul(0x9E3779B97F4A7C15));
    format!("{:012x}", n & 0xffff_ffff_ffff)
}
