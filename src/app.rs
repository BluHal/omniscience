//! The dashboard model and event loop. omni owns the live Claude terminals (one
//! PTY per tile); status comes from state.db (written by the agents' hooks),
//! polled each tick. The picker opens projects; a Lead can `omni spawn` agents
//! into its group via a db-queued request the loop picks up.

use crate::launch::{self, LaunchSpec};
use crate::term::Term;
use crate::{db, hooks};
use anyhow::Result;
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyModifiers, MouseButton, MouseEvent, MouseEventKind};
use ratatui::layout::Rect;
use rusqlite::Connection;
use std::path::PathBuf;
use std::time::{Duration, Instant};

/// How long to hold a first Esc before deciding it was lone (forward as interrupt)
/// or the start of an Esc-Esc detach (a second arrived → go to Nav, drop both).
const ESC_WINDOW: Duration = Duration::from_millis(400);

#[derive(PartialEq, Clone, Copy)]
pub enum Mode {
    Nav,
    Insert,
}

pub struct Tile {
    pub term: Term,
    pub id: String,
    pub group: String,
    pub role: String,
    pub is_lead: bool,
    pub status: String,
    pub activity: String,
    pub context_left: i64, // percent of context window free, -1 if unknown
    pub project: PathBuf,
    pub minimized: bool, // collapsed to the dock; its Claude keeps running
}

pub struct Proj {
    pub path: String,
    pub abs: PathBuf,
    pub branch: String,
}

pub struct Picker {
    pub query: String,
    pub all: Vec<Proj>,   // every git repo found under $HOME (scanned once)
    pub results: Vec<usize>, // indices into `all` matching the query
    pub cursor: usize,
    pub recent: std::collections::HashSet<String>, // display paths that are recents
}

impl Picker {
    fn refilter(&mut self) {
        let q = self.query.to_lowercase();
        self.results = (0..self.all.len())
            .filter(|&i| subseq(&q, &self.all[i].path.to_lowercase()))
            .collect();
        self.cursor = 0;
    }
    pub fn selected(&self) -> Option<&Proj> {
        self.results.get(self.cursor).map(|&i| &self.all[i])
    }
}

pub struct App {
    pub tiles: Vec<Tile>,
    pub focus: usize,
    pub home_sel: usize, // highlighted row on the welcome screen (when no tiles open)
    pub mode: Mode,
    pub glance: bool,
    pub zoom: std::collections::HashMap<String, i32>, // group name → column-width nudge (+/-/=)
    pub help: bool,
    pub blink: bool,
    pub picker: Option<Picker>,
    pub compose: Option<String>, // Some(text) while composing a broadcast (^b)
    pub should_quit: bool,
    db: Connection,
    settings: PathBuf,
    bus_dirs: std::collections::HashMap<String, PathBuf>, // group → its shared hcom bus dir
    last_poll: Instant,
    last_esc: Option<Instant>, // for Esc-Esc detach in Insert mode
    pub tile_areas: Vec<(usize, Rect)>, // populated each frame for mouse hit-testing
}

impl App {
    pub fn new() -> Result<App> {
        let db = db::open(&db::db_path())?;
        let omni_bin = std::env::current_exe()?.to_string_lossy().to_string();
        let settings = hooks::write_settings(&omni_bin)?;
        Ok(App {
            tiles: Vec::new(),
            focus: 0,
            home_sel: 0,
            mode: Mode::Nav,
            glance: false,
            zoom: std::collections::HashMap::new(),
            help: false,
            blink: false,
            picker: None,
            compose: None,
            should_quit: false,
            db,
            settings,
            bus_dirs: std::collections::HashMap::new(),
            last_poll: Instant::now() - Duration::from_secs(1),
            last_esc: None,
            tile_areas: Vec::new(),
        })
    }

    pub fn groups(&self) -> Vec<(String, Vec<usize>)> {
        let mut order: Vec<String> = Vec::new();
        let mut map: std::collections::HashMap<String, Vec<usize>> = std::collections::HashMap::new();
        for (i, t) in self.tiles.iter().enumerate() {
            if !map.contains_key(&t.group) {
                order.push(t.group.clone());
            }
            map.entry(t.group.clone()).or_default().push(i);
        }
        let mut out: Vec<(String, Vec<usize>)> =
            order.into_iter().map(|n| { let idxs = map.remove(&n).unwrap(); (n, idxs) }).collect();
        out.sort_by_key(|(_, idxs)| !idxs.iter().any(|&i| self.tiles[i].status == "blocked"));
        out
    }

    /// visible_groups is groups() with minimized tiles dropped (and groups that
    /// become empty removed) — the grid only draws what isn't in the dock.
    pub fn visible_groups(&self) -> Vec<(String, Vec<usize>)> {
        self.groups()
            .into_iter()
            .map(|(n, idxs)| (n, idxs.into_iter().filter(|&i| !self.tiles[i].minimized).collect::<Vec<_>>()))
            .filter(|(_, idxs)| !idxs.is_empty())
            .collect()
    }

    pub fn counts(&self) -> (usize, usize, usize) {
        let groups = self.groups().len();
        let agents = self.tiles.len();
        let blocked = self.tiles.iter().filter(|t| t.status == "blocked").count();
        (groups, agents, blocked)
    }

    pub fn focused(&self) -> Option<&Tile> {
        self.tiles.get(self.focus)
    }

    fn move_focus(&mut self, d: isize) {
        if self.tiles.is_empty() {
            return;
        }
        let n = self.tiles.len() as isize;
        self.focus = (((self.focus as isize) + d).rem_euclid(n)) as usize;
    }

    /// resize_focused widens (+) or narrows (-) the focused tile's group column by
    /// nudging its Fill weight relative to the other groups; reset_zoom clears it.
    fn resize_focused(&mut self, delta: i32) {
        if let Some(g) = self.focused().map(|t| t.group.clone()) {
            self.bump_zoom(&g, delta);
        }
    }

    fn reset_zoom(&mut self) {
        if let Some(g) = self.focused().map(|t| t.group.clone()) {
            self.zoom.remove(&g);
        }
    }

    /// bump_zoom adjusts a group's column nudge, clamped so a column can't vanish
    /// or swallow the row. ponytail: stale entries from closed groups are harmless
    /// (just unused), so no cleanup on group churn.
    fn bump_zoom(&mut self, group: &str, delta: i32) {
        let z = self.zoom.entry(group.to_string()).or_insert(0);
        *z = (*z + delta).clamp(-8, 12);
    }

    /// home_items is the selectable list on the welcome screen: row 0 opens the
    /// picker (None), the rest each open a recent project (Some(path)). The UI
    /// renders this and `home_sel` highlights one; Enter activates it.
    pub fn home_items(&self) -> Vec<Option<PathBuf>> {
        let mut items = vec![None];
        items.extend(crate::recents::list().into_iter().take(6).map(Some));
        items
    }

    fn home_move(&mut self, d: isize) {
        let n = self.home_items().len() as isize;
        if n <= 1 {
            self.home_sel = 0;
            return;
        }
        self.home_sel = (((self.home_sel as isize) + d).rem_euclid(n)) as usize;
    }

    fn activate_home(&mut self, cols: u16, rows: u16) {
        match self.home_items().get(self.home_sel).cloned() {
            Some(None) => self.open_picker(),
            Some(Some(dir)) if dir.is_dir() => {
                let _ = self.open_project(dir, cols, rows);
            }
            _ => {}
        }
    }

    fn jump_blocked(&mut self) {
        if let Some(i) = self.tiles.iter().position(|t| t.status == "blocked") {
            self.focus = i;
            self.tiles[i].minimized = false; // pull it out of the dock so you can answer
        }
    }

    /// close_focused ends the focused tile's Claude (Term's Drop kills the child)
    /// and forgets it from the restore set. Unlike minimize, the process stops.
    fn close_focused(&mut self) {
        if self.focus >= self.tiles.len() {
            return;
        }
        let id = self.tiles.remove(self.focus).id; // drop kills the PTY child
        let _ = db::delete_session(&self.db, &id);
        if self.focus >= self.tiles.len() && !self.tiles.is_empty() {
            self.focus = self.tiles.len() - 1;
        }
    }

    /// toggle_minimize collapses the focused tile to the dock (or restores it).
    /// The Claude process keeps running either way — only its tile is hidden.
    fn toggle_minimize(&mut self) {
        if let Some(t) = self.tiles.get_mut(self.focus) {
            t.minimized = !t.minimized;
        }
    }

    /// enter_insert focuses-to-type: restores the tile if it was minimized (you
    /// can't type into something you can't see), then switches to Insert.
    fn enter_insert(&mut self) {
        if let Some(t) = self.tiles.get_mut(self.focus) {
            t.minimized = false;
        }
        self.mode = Mode::Insert;
    }

    pub fn firstblocked_label(&self) -> Option<String> {
        self.tiles
            .iter()
            .find(|t| t.status == "blocked")
            .map(|t| format!("{}/{}", t.group, t.role))
    }

    // ---- status + spawn-request polling ----

    pub fn poll(&mut self, cols: u16, rows: u16) {
        if self.last_poll.elapsed() < Duration::from_millis(500) {
            return;
        }
        self.last_poll = Instant::now();
        self.blink = !self.blink;
        if let Ok(map) = db::statuses(&self.db) {
            for t in self.tiles.iter_mut() {
                if let Some(s) = map.get(&t.id) {
                    t.status = s.status.clone();
                    t.activity = s.activity.clone();
                    t.context_left = s.context_left;
                }
            }
        }
        if let Ok(reqs) = db::take_spawn_requests(&self.db) {
            for r in reqs {
                let _ = self.spawn_into(&r.room, &r.role, PathBuf::from(&r.project_path), Some(r.brief), cols, rows, false);
            }
        }
    }

    // ---- launch ----

    /// open_project spawns a Lead Claude in dir as a new group (group of one).
    pub fn open_project(&mut self, dir: PathBuf, cols: u16, rows: u16) -> Result<()> {
        let group = dir.file_name().map(|s| s.to_string_lossy().into_owned()).unwrap_or_else(|| "project".into());
        crate::recents::push(&dir);
        self.spawn_into(&group, "lead", dir, None, cols, rows, false)
    }

    /// restore_sessions re-launches the tiles that were open when omni last closed
    /// (db.sessions is kept in sync with the live tiles). Each agent resumes its
    /// own conversation via claude --continue. Called once at startup.
    pub fn restore_sessions(&mut self, cols: u16, rows: u16) {
        let saved = db::load_sessions(&self.db).unwrap_or_default();
        if saved.is_empty() {
            return;
        }
        let _ = db::clear_sessions(&self.db); // re-spawning writes fresh rows
        for s in saved {
            let _ = self.spawn_into(&s.room, &s.role, PathBuf::from(&s.project), None, cols, rows, true);
        }
        self.focus = 0;
    }

    /// reap_exited drops tiles whose Claude has quit and forgets their saved state,
    /// so the db keeps reflecting exactly the open tiles (the restore set).
    fn reap_exited(&mut self) {
        let mut dead = Vec::new();
        self.tiles.retain_mut(|t| {
            if t.term.exited() {
                dead.push(t.id.clone());
                false
            } else {
                true
            }
        });
        for id in dead {
            let _ = db::delete_session(&self.db, &id);
        }
        if self.focus >= self.tiles.len() && !self.tiles.is_empty() {
            self.focus = self.tiles.len() - 1;
        }
    }

    fn spawn_into(&mut self, group: &str, role: &str, dir: PathBuf, brief: Option<String>, cols: u16, rows: u16, resume: bool) -> Result<()> {
        let id = db::random_id();
        db::insert_session(&self.db, &id, group, &dir.to_string_lossy(), role)?;
        // The whole group shares ONE hcom bus: pin it to the first tile's dir so
        // cross-repo agents (omni spawn --dir elsewhere) still talk on one bus.
        let hcom_dir = self
            .bus_dirs
            .entry(group.to_string())
            .or_insert_with(|| launch::hcom_dir(&dir, group))
            .clone();
        let spec = LaunchSpec {
            dir: dir.clone(),
            id: id.clone(),
            room: group.to_string(),
            role: role.to_string(),
            settings: self.settings.clone(),
            hcom_dir,
            brief,
            resume,
        };
        let _ = std::fs::create_dir_all(&spec.hcom_dir);
        let term = Term::spawn(launch::claude_command(&spec), rows.max(4), cols.max(20))?;
        self.tiles.push(Tile {
            term,
            id,
            group: group.to_string(),
            role: role.to_string(),
            is_lead: role == "lead",
            status: "starting".into(),
            activity: String::new(),
            context_left: -1,
            project: dir,
            minimized: false,
        });
        self.focus = self.tiles.len() - 1;
        Ok(())
    }

    /// broadcast sends a message to every agent in the focused tile's group as a
    /// tagged decision (hcom broadcast within the room's bus).
    fn broadcast(&self, text: &str) {
        let Some(t) = self.focused() else { return };
        let hcom_dir = self
            .bus_dirs
            .get(&t.group)
            .cloned()
            .unwrap_or_else(|| launch::hcom_dir(&t.project, &t.group));
        let _ = std::process::Command::new("hcom")
            .args(["send", "--from", "omni", "--", text])
            .env("HCOM_DIR", hcom_dir)
            .output();
    }

    // ---- input ----

    pub fn on_key(&mut self, k: KeyEvent, cols: u16, rows: u16) -> Result<()> {
        if self.help {
            // any key dismisses help
            if !matches!(k.code, KeyCode::Char('c') if k.modifiers.contains(KeyModifiers::CONTROL)) {
                self.help = false;
            } else {
                self.should_quit = true;
            }
            return Ok(());
        }
        if self.picker.is_some() {
            return self.picker_key(k, cols, rows);
        }
        if self.compose.is_some() {
            self.compose_key(k);
            return Ok(());
        }
        match self.mode {
            Mode::Nav => self.nav_key(k, cols, rows),
            Mode::Insert => {
                let back = (k.code == KeyCode::Char('\\') && k.modifiers.contains(KeyModifiers::CONTROL))
                    || k.code == KeyCode::Char('\x1c'); // 0x1c = raw Ctrl+\ byte
                if back {
                    self.mode = Mode::Nav;
                    self.last_esc = None;
                } else if k.code == KeyCode::Esc {
                    // Esc-Esc (quick double-tap) detaches to nav WITHOUT interrupting
                    // Claude: the first Esc is HELD (not forwarded), a second within the
                    // window detaches and discards it, so the running turn is untouched.
                    // A lone Esc is flushed to Claude as an interrupt once the window
                    // closes (see flush_pending_esc). ponytail: 400ms window, flushed
                    // from the loop tick — the small interrupt delay is imperceptible.
                    let now = Instant::now();
                    if self.last_esc.is_some_and(|t| now.duration_since(t) < ESC_WINDOW) {
                        self.mode = Mode::Nav;
                        self.last_esc = None;
                    } else {
                        self.last_esc = Some(now); // held; flushed as a lone Esc if no second
                    }
                } else {
                    self.last_esc = None;
                    if let Some(t) = self.tiles.get_mut(self.focus) {
                        t.term.write_input(&encode_key(k));
                    }
                }
            }
        }
        Ok(())
    }

    /// flush_pending_esc forwards a held lone Esc to Claude once the Esc-Esc window
    /// closes (had a second Esc arrived it would have detached to Nav instead). Run
    /// each loop tick so a single Esc still reaches Claude as an interrupt.
    pub fn flush_pending_esc(&mut self) {
        if self.mode != Mode::Insert {
            self.last_esc = None;
            return;
        }
        if self.last_esc.is_some_and(|t| t.elapsed() >= ESC_WINDOW) {
            self.last_esc = None;
            if let Some(t) = self.tiles.get_mut(self.focus) {
                t.term.write_input(&[0x1b]);
            }
        }
    }

    fn nav_key(&mut self, k: KeyEvent, cols: u16, rows: u16) {
        let ctrl = k.modifiers.contains(KeyModifiers::CONTROL);
        let home = self.tiles.is_empty(); // the welcome screen is showing
        match k.code {
            KeyCode::Char('q') => self.should_quit = true,
            KeyCode::Char('c') if ctrl => self.should_quit = true,
            KeyCode::Char('?') => self.help = true,
            KeyCode::Char('n') if ctrl => self.open_picker(),
            KeyCode::Char('b') if ctrl => {
                if self.focused().is_some() {
                    self.compose = Some(String::new());
                }
            }
            KeyCode::Char('z') => self.glance = !self.glance,
            KeyCode::Char('+') => self.resize_focused(1),
            KeyCode::Char('-') => self.resize_focused(-1),
            KeyCode::Char('=') => self.reset_zoom(),
            KeyCode::Char('!') => self.jump_blocked(),
            KeyCode::Char('m') if !home => self.toggle_minimize(),
            KeyCode::Char('x') if !home => self.close_focused(),
            KeyCode::Char('i') => {
                if !home {
                    self.enter_insert();
                }
            }
            KeyCode::Enter => {
                if home {
                    self.activate_home(cols, rows);
                } else {
                    self.enter_insert();
                }
            }
            KeyCode::Tab | KeyCode::Right | KeyCode::Down => {
                if home {
                    self.home_move(1);
                } else {
                    self.move_focus(1);
                }
            }
            KeyCode::BackTab | KeyCode::Left | KeyCode::Up => {
                if home {
                    self.home_move(-1);
                } else {
                    self.move_focus(-1);
                }
            }
            _ => {}
        }
    }

    fn compose_key(&mut self, k: KeyEvent) {
        match k.code {
            KeyCode::Esc => self.compose = None,
            KeyCode::Enter => {
                if let Some(text) = self.compose.take() {
                    let text = text.trim().to_string();
                    if !text.is_empty() {
                        self.broadcast(&text);
                    }
                }
            }
            KeyCode::Backspace => {
                if let Some(s) = self.compose.as_mut() {
                    s.pop();
                }
            }
            KeyCode::Char(c) => {
                if let Some(s) = self.compose.as_mut() {
                    s.push(c);
                }
            }
            _ => {}
        }
    }

    pub fn on_mouse(&mut self, ev: MouseEvent) {
        let hit = self
            .tile_areas
            .iter()
            .find(|(_, r)| ev.column >= r.x && ev.column < r.x + r.width && ev.row >= r.y && ev.row < r.y + r.height)
            .map(|&(i, _)| i);
        let Some(i) = hit else { return };
        match ev.kind {
            MouseEventKind::Down(MouseButton::Left) => {
                self.focus = i;
                self.tiles[i].minimized = false; // clicking a dock chip restores it
            }
            MouseEventKind::ScrollUp => self.tiles[i].term.scroll(3),
            MouseEventKind::ScrollDown => self.tiles[i].term.scroll(-3),
            _ => {}
        }
    }

    fn open_picker(&mut self) {
        let mut all = find_projects();
        // surface recents first (in recency order), then the rest alphabetically
        let rec: Vec<String> = crate::recents::list().iter().map(|p| crate::recents::display(p)).collect();
        let rank = |path: &str| rec.iter().position(|r| r == path).map(|i| i as i64).unwrap_or(i64::MAX);
        all.sort_by(|a, b| rank(&a.path).cmp(&rank(&b.path)).then_with(|| a.path.cmp(&b.path)));
        let recent: std::collections::HashSet<String> = rec.into_iter().collect();
        let results = (0..all.len()).collect();
        self.picker = Some(Picker { query: String::new(), all, results, cursor: 0, recent });
    }

    fn picker_key(&mut self, k: KeyEvent, cols: u16, rows: u16) -> Result<()> {
        let mut launch_dir: Option<PathBuf> = None;
        {
            let p = self.picker.as_mut().unwrap();
            match k.code {
                KeyCode::Esc => {
                    self.picker = None;
                    return Ok(());
                }
                KeyCode::Up => p.cursor = p.cursor.saturating_sub(1),
                KeyCode::Down => {
                    if p.cursor + 1 < p.results.len() {
                        p.cursor += 1;
                    }
                }
                KeyCode::Enter => {
                    launch_dir = p.selected().map(|pr| pr.abs.clone());
                }
                KeyCode::Backspace => {
                    p.query.pop();
                    p.refilter();
                }
                KeyCode::Char(c) => {
                    p.query.push(c);
                    p.refilter();
                }
                _ => {}
            }
        }
        if let Some(dir) = launch_dir {
            self.picker = None;
            self.open_project(dir, cols, rows)?;
        }
        Ok(())
    }
}

/// find_projects walks $HOME (bounded depth, noise pruned) collecting every git
/// repo (a dir containing .git). Scanned once when the picker opens; filtering
/// is then in-memory. ponytail: a bounded BFS, not Spotlight — no deps, and a
/// repo isn't descended into, so it stays fast on a normal home.
pub fn find_projects() -> Vec<Proj> {
    let home = dirs::home_dir().unwrap_or_default();
    find_under(home.clone(), &home.to_string_lossy())
}

fn find_under(root: PathBuf, home_s: &str) -> Vec<Proj> {
    let prune: std::collections::HashSet<&str> = [
        "node_modules", ".git", "Library", ".Trash", "target", "vendor", "dist", "build",
        ".cargo", ".rustup", ".cache", "Applications", "Pictures", "Music", "Movies",
        ".npm", ".local", ".gem", ".cursor", ".vscode", ".venv", "venv", "__pycache__",
    ]
    .into_iter()
    .collect();
    let mut out = Vec::new();
    let mut stack = vec![(root, 0usize)];
    let mut visited = 0usize;
    while let Some((dir, depth)) = stack.pop() {
        if depth > 7 || visited > 30000 {
            continue;
        }
        visited += 1;
        if dir.join(".git").exists() {
            let disp = dir.to_string_lossy().replacen(home_s, "~", 1);
            let branch = launch::git_branch(&dir);
            out.push(Proj { path: disp, abs: dir, branch });
            continue; // don't descend into a repo
        }
        if let Ok(entries) = std::fs::read_dir(&dir) {
            for e in entries.flatten() {
                let nm = e.file_name();
                let nm = nm.to_string_lossy();
                if nm.starts_with('.') || prune.contains(nm.as_ref()) {
                    continue;
                }
                if e.path().is_dir() {
                    stack.push((e.path(), depth + 1));
                }
            }
        }
    }
    out.sort_by(|a, b| a.path.cmp(&b.path));
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_app() -> App {
        let db = Connection::open_in_memory().unwrap();
        App {
            tiles: Vec::new(),
            focus: 0,
            home_sel: 0,
            mode: Mode::Nav,
            glance: false,
            zoom: std::collections::HashMap::new(),
            help: false,
            blink: false,
            picker: None,
            compose: None,
            should_quit: false,
            db,
            settings: PathBuf::new(),
            bus_dirs: std::collections::HashMap::new(),
            last_poll: Instant::now(),
            last_esc: None,
            tile_areas: Vec::new(),
        }
    }

    // The welcome-screen cursor wraps within home_items, and row 0 is the picker.
    #[test]
    fn home_cursor_wraps_over_items() {
        let mut app = test_app();
        assert_eq!(app.home_items().first(), Some(&None)); // row 0 = open picker
        let n = app.home_items().len();
        app.home_sel = 0;
        app.home_move(-1);
        assert_eq!(app.home_sel, n - 1); // up from the top wraps to the bottom
        app.home_move(1);
        assert_eq!(app.home_sel, 0); // and back round to the top
    }

    // Esc-Esc detaches Insert→Nav; a lone Esc does not (it goes to Claude).
    #[test]
    fn double_esc_exits_insert() {
        let mut app = test_app();
        app.mode = Mode::Insert;
        let esc = KeyEvent::new(KeyCode::Esc, KeyModifiers::NONE);
        app.on_key(esc, 80, 24).unwrap();
        assert!(matches!(app.mode, Mode::Insert), "a single esc stays in insert");
        app.on_key(esc, 80, 24).unwrap();
        assert!(matches!(app.mode, Mode::Nav), "a quick double esc returns to nav");
    }

    // +/- nudge a group's column weight; the nudge clamps so a column can't
    // vanish or run away.
    #[test]
    fn resize_bumps_and_clamps_zoom() {
        let mut app = test_app();
        app.bump_zoom("alpha", 3);
        assert_eq!(app.zoom["alpha"], 3);
        app.bump_zoom("alpha", -5);
        assert_eq!(app.zoom["alpha"], -2);
        for _ in 0..40 { app.bump_zoom("alpha", 1); }
        assert_eq!(app.zoom["alpha"], 12, "clamps at the ceiling");
        for _ in 0..40 { app.bump_zoom("alpha", -1); }
        assert_eq!(app.zoom["alpha"], -8, "clamps at the floor");
    }

    // Headless render smoke test: the non-terminal screens (empty + recents,
    // help overlay, empty picker) must lay out without panicking.
    #[test]
    fn screens_render_without_panic() {
        use ratatui::backend::TestBackend;
        use ratatui::Terminal;
        let mut app = test_app();
        let mut term = Terminal::new(TestBackend::new(120, 40)).unwrap();
        term.draw(|f| crate::ui::draw(f, &mut app)).unwrap(); // empty
        app.help = true;
        term.draw(|f| crate::ui::draw(f, &mut app)).unwrap(); // help overlay
        app.help = false;
        app.picker = Some(Picker { query: "pay".into(), all: Vec::new(), results: Vec::new(), cursor: 0, recent: Default::default() });
        term.draw(|f| crate::ui::draw(f, &mut app)).unwrap(); // picker (no matches)
    }

    #[test]
    fn finds_git_repos_recursively() {
        let tmp = std::env::temp_dir().join(format!("omni-find-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(tmp.join("a/.git")).unwrap();
        std::fs::create_dir_all(tmp.join("nested/deep/b/.git")).unwrap();
        std::fs::create_dir_all(tmp.join("node_modules/pkg/.git")).unwrap(); // pruned
        std::fs::create_dir_all(tmp.join("plain")).unwrap(); // not a repo
        let found = find_under(tmp.clone(), &tmp.to_string_lossy());
        let names: Vec<String> = found.iter().map(|p| p.path.clone()).collect();
        assert!(names.iter().any(|n| n.ends_with("/a")), "should find a: {names:?}");
        assert!(names.iter().any(|n| n.ends_with("/b")), "should find nested b: {names:?}");
        assert!(!names.iter().any(|n| n.contains("node_modules")), "node_modules pruned: {names:?}");
        assert!(!names.iter().any(|n| n.ends_with("/plain")), "plain isn't a repo: {names:?}");
        let _ = std::fs::remove_dir_all(&tmp);
    }
}

fn subseq(q: &str, s: &str) -> bool {
    if q.is_empty() {
        return true;
    }
    let mut cur = q.chars();
    let mut want = cur.next();
    for c in s.chars() {
        if Some(c) == want {
            want = cur.next();
            if want.is_none() {
                return true;
            }
        }
    }
    want.is_none()
}

pub fn encode_key(k: KeyEvent) -> Vec<u8> {
    let ctrl = k.modifiers.contains(KeyModifiers::CONTROL);
    match k.code {
        KeyCode::Char(c) => {
            if ctrl {
                vec![(c.to_ascii_uppercase() as u8).wrapping_sub(0x40) & 0x1f]
            } else {
                c.to_string().into_bytes()
            }
        }
        KeyCode::Enter => vec![b'\r'],
        KeyCode::Backspace => vec![0x7f],
        KeyCode::Tab => vec![b'\t'],
        KeyCode::Esc => vec![0x1b],
        KeyCode::Up => b"\x1b[A".to_vec(),
        KeyCode::Down => b"\x1b[B".to_vec(),
        KeyCode::Right => b"\x1b[C".to_vec(),
        KeyCode::Left => b"\x1b[D".to_vec(),
        KeyCode::Home => b"\x1b[H".to_vec(),
        KeyCode::End => b"\x1b[F".to_vec(),
        _ => vec![],
    }
}

pub fn run_loop(terminal: &mut ratatui::DefaultTerminal) -> Result<()> {
    crossterm::execute!(std::io::stdout(), crossterm::event::EnableMouseCapture)?;
    let mut app = App::new()?;
    {
        let size = terminal.size()?;
        app.restore_sessions(size.width.saturating_sub(6), size.height.saturating_sub(6));
    }
    loop {
        terminal.draw(|f| crate::ui::draw(f, &mut app))?;
        if app.should_quit {
            break;
        }
        let size = terminal.size()?;
        let (tw, th) = (size.width.saturating_sub(6), size.height.saturating_sub(6));
        if event::poll(Duration::from_millis(16))? {
            match event::read()? {
                Event::Key(k) => app.on_key(k, tw, th)?,
                Event::Mouse(m) => app.on_mouse(m),
                _ => {}
            }
        }
        app.flush_pending_esc();
        app.poll(tw, th);
        app.reap_exited();
    }
    crossterm::execute!(std::io::stdout(), crossterm::event::DisableMouseCapture)?;
    Ok(())
}
