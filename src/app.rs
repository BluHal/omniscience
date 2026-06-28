//! The dashboard model and event loop. omni owns the live Claude terminals (one
//! PTY per tile); status comes from state.db (written by the agents' hooks),
//! polled each tick. The picker opens projects; a Lead can `omni spawn` agents
//! into its group via a db-queued request the loop picks up.

use crate::launch::{self, LaunchSpec};
use crate::term::Term;
use crate::{db, hooks};
use anyhow::Result;
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyModifiers};
use rusqlite::Connection;
use std::path::PathBuf;
use std::time::{Duration, Instant};

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
    pub project: PathBuf,
}

pub struct Proj {
    pub path: String,
    pub abs: PathBuf,
    pub branch: String,
}

pub struct Picker {
    pub query: String,
    pub results: Vec<Proj>,
    pub cursor: usize,
}

pub struct App {
    pub tiles: Vec<Tile>,
    pub focus: usize,
    pub mode: Mode,
    pub glance: bool,
    pub picker: Option<Picker>,
    pub compose: Option<String>, // Some(text) while composing a broadcast (^b)
    pub should_quit: bool,
    db: Connection,
    settings: PathBuf,
    last_poll: Instant,
    last_size: (u16, u16),
}

impl App {
    pub fn new() -> Result<App> {
        let db = db::open(&db::db_path())?;
        let omni_bin = std::env::current_exe()?.to_string_lossy().to_string();
        let settings = hooks::write_settings(&omni_bin)?;
        Ok(App {
            tiles: Vec::new(),
            focus: 0,
            mode: Mode::Nav,
            glance: false,
            picker: None,
            compose: None,
            should_quit: false,
            db,
            settings,
            last_poll: Instant::now() - Duration::from_secs(1),
            last_size: (80, 24),
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

    fn jump_blocked(&mut self) {
        if let Some(i) = self.tiles.iter().position(|t| t.status == "blocked") {
            self.focus = i;
        }
    }

    // ---- status + spawn-request polling ----

    pub fn poll(&mut self, cols: u16, rows: u16) {
        self.last_size = (cols, rows);
        if self.last_poll.elapsed() < Duration::from_millis(500) {
            return;
        }
        self.last_poll = Instant::now();
        if let Ok(map) = db::statuses(&self.db) {
            for t in self.tiles.iter_mut() {
                if let Some(s) = map.get(&t.id) {
                    t.status = s.status.clone();
                    t.activity = s.activity.clone();
                }
            }
        }
        if let Ok(reqs) = db::take_spawn_requests(&self.db) {
            for r in reqs {
                let _ = self.spawn_into(&r.room, &r.role, PathBuf::from(&r.project_path), Some(r.brief), cols, rows);
            }
        }
    }

    // ---- launch ----

    /// open_project spawns a Lead Claude in dir as a new group (group of one).
    pub fn open_project(&mut self, dir: PathBuf, cols: u16, rows: u16) -> Result<()> {
        let group = dir.file_name().map(|s| s.to_string_lossy().into_owned()).unwrap_or_else(|| "project".into());
        self.spawn_into(&group, "lead", dir, None, cols, rows)
    }

    fn spawn_into(&mut self, group: &str, role: &str, dir: PathBuf, brief: Option<String>, cols: u16, rows: u16) -> Result<()> {
        let id = db::random_id();
        db::insert_session(&self.db, &id, group, &dir.to_string_lossy(), role)?;
        let spec = LaunchSpec {
            dir: dir.clone(),
            id: id.clone(),
            room: group.to_string(),
            role: role.to_string(),
            settings: self.settings.clone(),
            hcom_dir: launch::hcom_dir(&dir, group),
            brief,
        };
        let _ = std::fs::create_dir_all(&spec.hcom_dir);
        let term = Term::spawn(launch::claude_command(&spec), rows.max(4), cols.max(20), group)?;
        self.tiles.push(Tile {
            term,
            id,
            group: group.to_string(),
            role: role.to_string(),
            is_lead: role == "lead",
            status: "starting".into(),
            activity: String::new(),
            project: dir,
        });
        self.focus = self.tiles.len() - 1;
        Ok(())
    }

    /// broadcast sends a message to every agent in the focused tile's group as a
    /// tagged decision (hcom broadcast within the room's bus).
    fn broadcast(&self, text: &str) {
        let Some(t) = self.focused() else { return };
        let hcom_dir = launch::hcom_dir(&t.project, &t.group);
        let _ = std::process::Command::new("hcom")
            .args(["send", "--from", "omni", "--", text])
            .env("HCOM_DIR", hcom_dir)
            .output();
    }

    // ---- input ----

    pub fn on_key(&mut self, k: KeyEvent, cols: u16, rows: u16) -> Result<()> {
        if self.picker.is_some() {
            return self.picker_key(k, cols, rows);
        }
        if self.compose.is_some() {
            self.compose_key(k);
            return Ok(());
        }
        match self.mode {
            Mode::Nav => self.nav_key(k),
            Mode::Insert => {
                if k.code == KeyCode::Char('\\') && k.modifiers.contains(KeyModifiers::CONTROL) {
                    self.mode = Mode::Nav;
                } else if let Some(t) = self.tiles.get_mut(self.focus) {
                    t.term.write_input(&encode_key(k));
                }
            }
        }
        Ok(())
    }

    fn nav_key(&mut self, k: KeyEvent) {
        let ctrl = k.modifiers.contains(KeyModifiers::CONTROL);
        match k.code {
            KeyCode::Char('q') => self.should_quit = true,
            KeyCode::Char('c') if ctrl => self.should_quit = true,
            KeyCode::Char('n') if ctrl => self.open_picker(),
            KeyCode::Char('b') if ctrl => {
                if self.focused().is_some() {
                    self.compose = Some(String::new());
                }
            }
            KeyCode::Char('z') => self.glance = !self.glance,
            KeyCode::Char('!') => self.jump_blocked(),
            KeyCode::Char('i') | KeyCode::Enter => {
                if !self.tiles.is_empty() {
                    self.mode = Mode::Insert;
                }
            }
            KeyCode::Tab | KeyCode::Right | KeyCode::Down => self.move_focus(1),
            KeyCode::BackTab | KeyCode::Left | KeyCode::Up => self.move_focus(-1),
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

    fn open_picker(&mut self) {
        self.picker = Some(Picker { query: String::new(), results: scan_projects(""), cursor: 0 });
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
                    if let Some(proj) = p.results.get(p.cursor) {
                        launch_dir = Some(proj.abs.clone());
                    }
                }
                KeyCode::Backspace => {
                    p.query.pop();
                    p.results = scan_projects(&p.query);
                    p.cursor = 0;
                }
                KeyCode::Char(c) => {
                    p.query.push(c);
                    p.results = scan_projects(&p.query);
                    p.cursor = 0;
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

pub fn scan_projects(query: &str) -> Vec<Proj> {
    let home = dirs::home_dir().unwrap_or_default();
    let home_s = home.to_string_lossy().into_owned();
    let mut out = Vec::new();
    for root in launch::project_roots() {
        let entries = match std::fs::read_dir(&root) {
            Ok(e) => e,
            Err(_) => continue,
        };
        for e in entries.flatten() {
            if !e.path().is_dir() {
                continue;
            }
            let name = e.file_name().to_string_lossy().into_owned();
            if name.starts_with('.') {
                continue;
            }
            let abs = e.path();
            let disp = abs.to_string_lossy().replacen(&home_s, "~", 1);
            if !subseq(&query.to_lowercase(), &disp.to_lowercase()) {
                continue;
            }
            let branch = launch::git_branch(&abs);
            out.push(Proj { path: disp, abs, branch });
        }
    }
    out.sort_by(|a, b| a.path.cmp(&b.path));
    out.truncate(12);
    out
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
    let mut app = App::new()?;
    loop {
        terminal.draw(|f| crate::ui::draw(f, &mut app))?;
        if app.should_quit {
            break;
        }
        let size = terminal.size()?;
        let (tw, th) = (size.width.saturating_sub(6), size.height.saturating_sub(6));
        if event::poll(Duration::from_millis(16))? {
            if let Event::Key(k) = event::read()? {
                app.on_key(k, tw, th)?;
            }
        }
        app.poll(tw, th);
        app.tiles.retain_mut(|t| !t.term.exited());
        if app.focus >= app.tiles.len() && !app.tiles.is_empty() {
            app.focus = app.tiles.len() - 1;
        }
    }
    Ok(())
}
