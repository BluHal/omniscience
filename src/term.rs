//! An embedded live terminal: a child process running in a PTY, its output fed
//! to a vt100 parser by a reader thread. The UI renders the parser's screen
//! (via tui-term) and forwards keystrokes to the PTY writer. This is what makes
//! each tile a real, interactive terminal.

use anyhow::Result;
use portable_pty::{native_pty_system, Child, CommandBuilder, MasterPty, PtySize};
use std::io::{Read, Write};
use std::sync::{Arc, Mutex};
use std::thread;

pub struct Term {
    parser: Arc<Mutex<vt100::Parser>>,
    master: Box<dyn MasterPty + Send>,
    writer: Box<dyn Write + Send>,
    child: Box<dyn Child + Send + Sync>,
    pub rows: u16,
    pub cols: u16,
}

impl Term {
    /// spawn starts cmd in a fresh PTY sized rows×cols and begins streaming its
    /// output into a vt100 parser on a background thread.
    pub fn spawn(cmd: CommandBuilder, rows: u16, cols: u16) -> Result<Term> {
        let rows = rows.max(1);
        let cols = cols.max(1);
        let pty = native_pty_system();
        let pair = pty.openpty(PtySize { rows, cols, pixel_width: 0, pixel_height: 0 })?;
        let child = pair.slave.spawn_command(cmd)?;
        drop(pair.slave); // close our handle to the slave so EOF propagates on exit

        let parser = Arc::new(Mutex::new(vt100::Parser::new(rows, cols, 10_000)));
        let mut reader = pair.master.try_clone_reader()?;
        let writer = pair.master.take_writer()?;
        {
            let parser = parser.clone();
            thread::spawn(move || {
                let mut buf = [0u8; 8192];
                loop {
                    match reader.read(&mut buf) {
                        Ok(0) | Err(_) => break,
                        Ok(n) => {
                            if let Ok(mut p) = parser.lock() {
                                p.process(&buf[..n]);
                            }
                        }
                    }
                }
            });
        }
        Ok(Term { parser, master: pair.master, writer, child, rows, cols })
    }

    /// resize the PTY and parser to match a new tile size (idempotent).
    pub fn resize(&mut self, rows: u16, cols: u16) {
        let rows = rows.max(1);
        let cols = cols.max(1);
        if rows == self.rows && cols == self.cols {
            return;
        }
        let _ = self.master.resize(PtySize { rows, cols, pixel_width: 0, pixel_height: 0 });
        if let Ok(mut p) = self.parser.lock() {
            p.set_size(rows, cols);
        }
        self.rows = rows;
        self.cols = cols;
    }

    /// write_input forwards raw bytes (encoded keystrokes) to the child. Typing
    /// snaps the view back to live (out of any scrollback).
    pub fn write_input(&mut self, bytes: &[u8]) {
        if let Ok(mut p) = self.parser.lock() {
            p.set_scrollback(0);
        }
        let _ = self.writer.write_all(bytes);
        let _ = self.writer.flush();
    }

    /// scroll moves the vt100 scrollback view by delta rows (positive = back into
    /// history, negative = toward live); clamped at the live edge.
    pub fn scroll(&self, delta: isize) {
        if let Ok(mut p) = self.parser.lock() {
            let cur = p.screen().scrollback() as isize;
            p.set_scrollback((cur + delta).max(0) as usize);
        }
    }

    pub fn parser(&self) -> Arc<Mutex<vt100::Parser>> {
        self.parser.clone()
    }

    /// exited reports whether the child has finished.
    pub fn exited(&mut self) -> bool {
        matches!(self.child.try_wait(), Ok(Some(_)))
    }
}

impl Drop for Term {
    fn drop(&mut self) {
        let _ = self.child.kill();
    }
}
