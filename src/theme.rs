//! The Claude Design "Terminal Dashboard UI System" palette and glyphs, as
//! ratatui colors. Exact CSS-var hexes from the .dc.html screens.

use ratatui::style::Color;

pub const SCREEN: Color = Color::Rgb(0x0a, 0x0e, 0x16);
pub const PANEL: Color = Color::Rgb(0x0c, 0x11, 0x1b);
pub const INSET: Color = Color::Rgb(0x0e, 0x14, 0x20);
pub const BAR: Color = Color::Rgb(0x0e, 0x13, 0x1e);
pub const BD: Color = Color::Rgb(0x28, 0x32, 0x46);
pub const BD2: Color = Color::Rgb(0x1a, 0x22, 0x31);
pub const GRP: Color = Color::Rgb(0x3c, 0x4d, 0x6b);
pub const TXT: Color = Color::Rgb(0xc4, 0xce, 0xe0);
pub const DIM: Color = Color::Rgb(0x6b, 0x78, 0x91);
pub const FAINT: Color = Color::Rgb(0x41, 0x4c, 0x62);
pub const WORK: Color = Color::Rgb(0x4f, 0xd6, 0xdb);
pub const BLOCK: Color = Color::Rgb(0xff, 0x53, 0x47);
pub const IDLE: Color = Color::Rgb(0x94, 0xa3, 0xd4);
pub const DONE: Color = Color::Rgb(0x4f, 0xd6, 0xa0);
pub const FOCUS: Color = Color::Rgb(0x79, 0xe9, 0xee);
pub const CAST: Color = Color::Rgb(0xf5, 0x6f, 0xd0);

// Glyphs
pub const BRAND: &str = "◆";
pub const BLOCKED: &str = "◍";
pub const WORKING: &str = "●";
pub const IDLE_G: &str = "○";
pub const DONE_G: &str = "✓";
pub const FOCUS_G: &str = "⌖";
pub const CAST_G: &str = "✷";
pub const ROW: &str = "▸";
pub const RECENT: &str = "↺";
pub const SEARCH: &str = "⌕";

/// status_color maps a session status to its design color.
pub fn status_color(status: &str) -> Color {
    match status {
        "working" | "starting" => WORK,
        "blocked" => BLOCK,
        "idle" => IDLE,
        "done" => DONE,
        _ => DIM,
    }
}

/// status_glyph maps a session status to its design glyph.
pub fn status_glyph(status: &str) -> &'static str {
    match status {
        "blocked" => BLOCKED,
        "idle" => IDLE_G,
        "done" => DONE_G,
        _ => WORKING,
    }
}
