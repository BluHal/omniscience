//! The dashboard model and event loop. omni owns the live Claude terminals
//! (one PTY per tile), so the in-memory tile list is the source of truth; the
//! picker opens new projects, focus/insert route the keyboard.

use crate::launch;
use crate::term::Term;
use anyhow::Result;
use crossterm::event::{self, Event, KeyCode, KeyEvent, KeyModifiers};
use std::path::PathBuf;
use std::time::Duration;

#[derive(PartialEq, Clone, Copy)]
pub enum Mode {
    Nav,
    Insert,
}

pub struct Tile {
    pub term: Term,
    pub group: String,
    pub role: String,
    pub is_lead: bool,
    pub status: String, // working | blocked | idle | done (placeholder until hooks land)
}

pub struct Proj {
    pub path: String, // ~/-relativized display
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
    pub should_quit: bool,
}

impl App {
    pub fn new() -> App {
        App {
            tiles: Vec::new(),
            focus: 0,
            mode: Mode::Nav,
            glance: false,
            picker: None,
            should_quit: false,
        }
    }

    /// groups returns ordered (name, tile-indices) preserving insertion order,
    /// blocked groups floated to the front.
    pub fn groups(&self) -> Vec<(String, Vec<usize>)> {
        let mut order: Vec<String> = Vec::new();
        let mut map: std::collections::HashMap<String, Vec<usize>> = std::collections::HashMap::new();
        for (i, t) in self.tiles.iter().enumerate() {
            if !map.contains_key(&t.group) {
                order.push(t.group.clone());
            }
            map.entry(t.group.clone()).or_default().push(i);
        }
        let mut out: Vec<(String, Vec<usize>)> = order.into_iter().map(|n| {
            let idxs = map.remove(&n).unwrap();
            (n, idxs)
        }).collect();
        out.sort_by_key(|(_, idxs)| {
            let blocked = idxs.iter().any(|&i| self.tiles[i].status == "blocked");
            !blocked // false(0) sorts before true(1): blocked groups first
        });
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
        self.focus = (((self.focus as isize) + d % n + n) % n) as usize;
    }

    fn jump_blocked(&mut self) {
        if let Some(i) = self.tiles.iter().position(|t| t.status == "blocked") {
            self.focus = i;
        }
    }

    // ---- launch ----

    /// open_project spawns a Lead Claude in dir as a new group (group of one).
    pub fn open_project(&mut self, dir: PathBuf, cols: u16, rows: u16) -> Result<()> {
        let group = dir
            .file_name()
            .map(|s| s.to_string_lossy().into_owned())
            .unwrap_or_else(|| "project".into());
        let term = Term::spawn(launch::claude_command(&dir), rows, cols, group.clone())?;
        self.tiles.push(Tile {
            term,
            group,
            role: "lead".into(),
            is_lead: true,
            status: "working".into(),
        });
        self.focus = self.tiles.len() - 1;
        Ok(())
    }

    // ---- input ----

    pub fn on_key(&mut self, k: KeyEvent, tile_cols: u16, tile_rows: u16) -> Result<()> {
        if self.picker.is_some() {
            self.picker_key(k, tile_cols, tile_rows)?;
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

    fn open_picker(&mut self) {
        let mut p = Picker { query: String::new(), results: Vec::new(), cursor: 0 };
        p.results = scan_projects("");
        self.picker = Some(p);
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

/// scan_projects lists immediate child dirs of the project roots, subsequence-
/// filtered by query, capped.
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
    let mut qi = q.chars();
    let mut cur = qi.next();
    for c in s.chars() {
        if Some(c) == cur {
            cur = qi.next();
            if cur.is_none() {
                return true;
            }
        }
    }
    cur.is_none()
}

/// encode_key turns a crossterm key event into the bytes a PTY expects.
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

/// run_loop drives the TUI until quit.
pub fn run_loop(terminal: &mut ratatui::DefaultTerminal) -> Result<()> {
    let mut app = App::new();
    loop {
        terminal.draw(|f| crate::ui::draw(f, &mut app))?;
        if app.should_quit {
            break;
        }
        if event::poll(Duration::from_millis(16))? {
            match event::read()? {
                Event::Key(k) => {
                    // a representative tile size for newly-spawned terminals
                    let size = terminal.size()?;
                    app.on_key(k, size.width.saturating_sub(6), size.height.saturating_sub(6))?;
                }
                Event::Resize(_, _) => {}
                _ => {}
            }
        }
        // drop tiles whose child exited
        app.tiles.retain_mut(|t| !t.term.exited());
        if app.focus >= app.tiles.len() && !app.tiles.is_empty() {
            app.focus = app.tiles.len() - 1;
        }
    }
    Ok(())
}
