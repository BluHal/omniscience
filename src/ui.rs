//! Rendering — the Claude Design states drawn with ratatui, with live embedded
//! terminals (tui-term) in the tile bodies.

use crate::app::{App, Mode, Tile};
use crate::theme::*;
use ratatui::layout::{Alignment, Constraint, Layout, Rect};
use ratatui::style::{Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, BorderType, Borders, Clear, Paragraph};
use ratatui::Frame;
use tui_term::widget::PseudoTerminal;

fn sty(c: ratatui::style::Color) -> Style {
    Style::default().fg(c)
}
fn bold(c: ratatui::style::Color) -> Style {
    Style::default().fg(c).add_modifier(Modifier::BOLD)
}

pub fn draw(f: &mut Frame, app: &mut App) {
    let area = f.area();
    f.render_widget(Block::default().style(Style::default().bg(SCREEN)), area);
    let rows = Layout::vertical([Constraint::Length(1), Constraint::Min(0), Constraint::Length(1)]).split(area);
    top_bar(f, app, rows[0]);
    let body = rows[1];

    let picker_open = app.picker.is_some();
    if app.tiles.is_empty() && !picker_open {
        empty(f, body);
    } else if app.glance && !picker_open {
        glance(f, app, body);
    } else {
        hero(f, app, body);
    }
    if app.compose.is_some() {
        compose_bar(f, app, rows[2]);
    } else {
        footer(f, app, rows[2]);
    }
    if picker_open {
        picker(f, app, body);
    }
}

fn compose_bar(f: &mut Frame, app: &App, area: Rect) {
    f.render_widget(Block::default().style(Style::default().bg(BAR)), area);
    let group = app.focused().map(|t| t.group.clone()).unwrap_or_default();
    let text = app.compose.clone().unwrap_or_default();
    let line = Line::from(vec![
        Span::styled(format!(" {} broadcast → {}: ", CAST_G, group), bold(CAST)),
        Span::styled(text, sty(TXT)),
        Span::styled("▌", sty(CAST)),
        Span::styled("   ⏎ send · esc cancel", sty(FAINT)),
    ]);
    let pad = Rect { x: area.x + 1, width: area.width.saturating_sub(2), ..area };
    f.render_widget(Paragraph::new(line).style(Style::default().bg(BAR)), pad);
}

fn top_bar(f: &mut Frame, app: &App, area: Rect) {
    f.render_widget(Block::default().style(Style::default().bg(BAR)), area);
    let (g, a, blk) = app.counts();
    let sub = if app.glance { "glance mode · 俯瞰" } else { "~/work" };
    let left = Line::from(vec![
        Span::styled(format!("{} omni", BRAND), bold(FOCUS)),
        Span::raw(" "),
        Span::styled("オムニ", sty(DIM)),
        Span::styled("  ·  ", sty(FAINT)),
        Span::styled(sub, sty(DIM)),
    ]);
    let mut right = vec![
        Span::styled(format!("{}", g), bold(IDLE)),
        Span::styled(" groups", sty(FAINT)),
        Span::styled("  ·  ", sty(FAINT)),
        Span::styled(format!("{}", a), bold(WORK)),
        Span::styled(" agents", sty(FAINT)),
        Span::styled("  ·  ", sty(FAINT)),
    ];
    if blk > 0 {
        right.push(Span::styled(format!("{} {} blocked", BLOCKED, blk), bold(BLOCK)));
    } else {
        right.push(Span::styled("○ none blocked", sty(FAINT)));
    }
    let pad = Rect { x: area.x + 1, width: area.width.saturating_sub(2), ..area };
    f.render_widget(Paragraph::new(left).style(Style::default().bg(BAR)), pad);
    f.render_widget(Paragraph::new(Line::from(right)).alignment(Alignment::Right).style(Style::default().bg(BAR)), pad);
}

fn footer(f: &mut Frame, app: &App, area: Rect) {
    f.render_widget(Block::default().style(Style::default().bg(BAR)), area);
    let hint = |k: &str, l: &str, c| {
        vec![Span::styled(k.to_string(), bold(c)), Span::raw(" "), Span::styled(l.to_string(), sty(DIM)), Span::raw("   ")]
    };
    let mut spans = Vec::new();
    if app.picker.is_some() {
        for s in hint("↑↓", "move", TXT) { spans.push(s); }
        for s in hint("⏎", "open", TXT) { spans.push(s); }
        for s in hint("esc", "cancel", TXT) { spans.push(s); }
    } else if app.mode == Mode::Insert {
        for s in hint("^\\", "back to nav", FOCUS) { spans.push(s); }
        spans.push(Span::styled("typing into focused tile…", sty(DIM)));
    } else {
        for s in hint("↹", "focus", TXT) { spans.push(s); }
        for s in hint("i/⏎", "type", TXT) { spans.push(s); }
        for s in hint("z", "glance", TXT) { spans.push(s); }
        for s in hint("^n", "new project", TXT) { spans.push(s); }
        for s in hint("!", "jump blocked", BLOCK) { spans.push(s); }
        for s in hint("q", "quit", TXT) { spans.push(s); }
    }
    let pad = Rect { x: area.x + 1, width: area.width.saturating_sub(2), ..area };
    f.render_widget(Paragraph::new(Line::from(spans)).style(Style::default().bg(BAR)), pad);
}

fn empty(f: &mut Frame, area: Rect) {
    let lines = vec![
        Line::from(vec![Span::styled("omni", bold(TXT)), Span::raw("  "), Span::styled("オムニ", sty(DIM))]).alignment(Alignment::Center),
        Line::raw(""),
        Line::from(Span::styled("run & watch many live coding sessions, side by side", sty(DIM))).alignment(Alignment::Center),
        Line::raw(""),
        Line::from(vec![Span::styled("⏵ ", sty(FOCUS)), Span::styled("^n", bold(TXT)), Span::styled("  open a project", sty(TXT))]),
        Line::from(vec![Span::styled("⏎ ", sty(DIM)), Span::styled("i", bold(TXT)), Span::styled("   type into the focused tile", sty(DIM))]),
        Line::raw(""),
        Line::from(Span::styled("LEGEND  ● working  ◍ blocked  ○ idle  ✓ done  ⌖ focus", sty(FAINT))).alignment(Alignment::Center),
    ];
    let w = 64.min(area.width.saturating_sub(4));
    let h = (lines.len() as u16 + 2).min(area.height);
    let card = centered(area, w, h);
    f.render_widget(Clear, card);
    let block = Block::bordered().border_type(BorderType::Rounded).border_style(sty(BD2)).style(Style::default().bg(SCREEN));
    let inner = block.inner(card);
    f.render_widget(block, card);
    f.render_widget(Paragraph::new(lines), Rect { x: inner.x + 2, width: inner.width.saturating_sub(4), ..inner });
}

fn hero(f: &mut Frame, app: &mut App, area: Rect) {
    let groups = app.groups();
    if groups.is_empty() {
        return;
    }
    let constraints: Vec<Constraint> = groups
        .iter()
        .map(|(_, idxs)| Constraint::Fill(1 + idxs.len().saturating_sub(1) as u16))
        .collect();
    let cols = Layout::horizontal(constraints).spacing(1).split(area);
    for (gi, (name, idxs)) in groups.iter().enumerate() {
        group_frame(f, app, name, idxs, cols[gi], false);
    }
}

fn glance(f: &mut Frame, app: &mut App, area: Rect) {
    let groups = app.groups();
    if groups.is_empty() {
        return;
    }
    let constraints: Vec<Constraint> = groups.iter().map(|_| Constraint::Fill(1)).collect();
    let cols = Layout::horizontal(constraints).spacing(1).split(area);
    for (gi, (name, idxs)) in groups.iter().enumerate() {
        group_frame(f, app, name, idxs, cols[gi], true);
    }
}

fn group_frame(f: &mut Frame, app: &mut App, name: &str, idxs: &[usize], area: Rect, compact: bool) {
    let blocked = idxs.iter().any(|&i| app.tiles[i].status == "blocked");
    let role = if idxs.len() <= 1 { "lead".to_string() } else { format!("lead+{}", idxs.len() - 1) };
    let left_c = if blocked { BLOCK } else { IDLE };
    let left = Line::from(vec![
        Span::styled(name.to_string(), Style::default().fg(left_c).add_modifier(if blocked { Modifier::BOLD } else { Modifier::empty() })),
        Span::styled(format!(" · {}", role), sty(FAINT)),
    ]);
    let right_label = if blocked {
        Line::from(Span::styled(format!("{} needs you", idxs.iter().filter(|&&i| app.tiles[i].status == "blocked").count()), sty(BLOCK)))
    } else if idxs.len() > 1 {
        Line::from(Span::styled(format!("{} message bus", CAST_G), sty(CAST)))
    } else {
        Line::from(Span::styled("1 session", sty(FAINT)))
    };
    let block = Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(sty(GRP))
        .title_top(left.left_aligned())
        .title_top(right_label.right_aligned());
    let inner = block.inner(area);
    f.render_widget(block, area);

    // lay out tiles within the group: lone tile fills; else lead left + agents right
    let lead_pos = idxs.iter().position(|&i| app.tiles[i].is_lead).unwrap_or(0);
    if idxs.len() == 1 {
        tile(f, app, idxs[0], inner, compact);
        return;
    }
    let parts = Layout::horizontal([Constraint::Percentage(52), Constraint::Percentage(48)]).spacing(1).split(inner);
    tile(f, app, idxs[lead_pos], parts[0], compact);
    let others: Vec<usize> = idxs.iter().cloned().filter(|&i| i != idxs[lead_pos]).collect();
    if !others.is_empty() {
        let rc: Vec<Constraint> = others.iter().map(|_| Constraint::Fill(1)).collect();
        let rows = Layout::vertical(rc).spacing(1).split(parts[1]);
        for (k, &i) in others.iter().enumerate() {
            tile(f, app, i, rows[k], compact);
        }
    }
}

fn tile(f: &mut Frame, app: &mut App, i: usize, area: Rect, compact: bool) {
    if area.height < 2 || area.width < 2 {
        return;
    }
    let focused = i == app.focus;
    let status = app.tiles[i].status.clone();
    let header = tile_header(&app.tiles[i], focused);
    let bt = if focused { BorderType::Double } else { BorderType::Rounded };
    let bc = if focused { FOCUS } else if status == "blocked" { BLOCK } else { BD };
    let block = Block::default()
        .borders(Borders::ALL)
        .border_type(bt)
        .border_style(sty(bc))
        .title_top(header.0.left_aligned())
        .title_top(header.1.right_aligned())
        .style(Style::default().bg(PANEL));
    let inner = block.inner(area);

    if compact {
        f.render_widget(block, area);
        let t = &app.tiles[i];
        let body = Paragraph::new(vec![
            Line::from(Span::styled(format!(" {}", summary(t)), sty(DIM))),
            Line::from(Span::styled(format!(" ⏵ {}", t.role), sty(FAINT))),
        ]);
        f.render_widget(body, inner);
        return;
    }

    let t = &mut app.tiles[i];
    t.term.resize(inner.height, inner.width);
    let parser = t.term.parser();
    let guard = parser.lock().unwrap();
    let pt = PseudoTerminal::new(guard.screen()).block(block);
    f.render_widget(pt, area);
}

fn tile_header(t: &Tile, focused: bool) -> (Line<'static>, Line<'static>) {
    let sc = status_color(&t.status);
    let mut left = Vec::new();
    if t.status == "blocked" {
        left.push(Span::styled(format!("{} ", BLOCKED), sty(BLOCK)));
        left.push(Span::styled(t.role.clone(), bold(BLOCK)));
    } else if focused {
        left.push(Span::styled(format!("{} {} ", FOCUS_G, BRAND), sty(FOCUS)));
        left.push(Span::styled(t.role.clone(), bold(FOCUS)));
    } else {
        left.push(Span::styled(format!("{} ", status_glyph(&t.status)), sty(sc)));
        left.push(Span::styled(t.role.clone(), bold(TXT)));
    }
    if t.is_lead {
        left.push(Span::styled(" ⟦LEAD⟧", sty(if focused { FOCUS } else { IDLE })));
    }
    let right = if t.status == "blocked" {
        Line::from(Span::styled(" BLOCKED 要対応 ", Style::default().bg(BLOCK).fg(SCREEN).add_modifier(Modifier::BOLD)))
    } else {
        Line::from(vec![
            Span::styled(format!("{} {}", status_glyph(&t.status), t.status), sty(sc)),
        ])
    };
    (Line::from(left), right)
}

fn summary(t: &Tile) -> String {
    match t.status.as_str() {
        "idle" => "waiting · ready for next task".into(),
        "done" => "complete".into(),
        _ => "working".into(),
    }
}

fn picker(f: &mut Frame, app: &App, area: Rect) {
    let p = app.picker.as_ref().unwrap();
    let w = 72.min(area.width.saturating_sub(8));
    let h = (p.results.len().min(8) as u16 + 5).min(area.height.saturating_sub(2));
    let modal = Rect { x: area.x + (area.width.saturating_sub(w)) / 2, y: area.y + 3, width: w, height: h };
    f.render_widget(Clear, modal);
    let block = Block::bordered()
        .border_type(BorderType::Rounded)
        .border_style(sty(FOCUS))
        .style(Style::default().bg(PANEL))
        .title_top(Line::from(vec![Span::styled(format!("{} open project", SEARCH), bold(TXT)), Span::styled("  プロジェクトを開く", sty(FAINT))]).left_aligned())
        .title_top(Line::from(Span::styled("scan ~/work ~/src ~/dev", sty(FAINT))).right_aligned());
    let inner = block.inner(modal);
    f.render_widget(block, modal);

    let qline = Line::from(vec![
        Span::styled(format!(" ⏵ {}", p.query), sty(TXT)),
        Span::styled("▌", sty(FOCUS)),
    ]);
    let chunks = Layout::vertical([Constraint::Length(1), Constraint::Length(1), Constraint::Min(0)]).split(inner);
    f.render_widget(Paragraph::new(qline), chunks[0]);
    f.render_widget(Paragraph::new(Line::from(Span::styled(" ─".repeat((inner.width / 2) as usize), sty(BD2)))), chunks[1]);

    let mut rows = Vec::new();
    for (i, pr) in p.results.iter().take(8).enumerate() {
        let sel = i == p.cursor;
        let marker = if sel { Span::styled(format!(" {} ", ROW), bold(FOCUS)) } else { Span::styled(format!(" {} ", ROW), sty(FAINT)) };
        let name = if sel { Span::styled(pr.path.clone(), bold(TXT)) } else { Span::styled(pr.path.clone(), sty(TXT)) };
        let branch = Span::styled(format!("  {} {}", if sel { WORKING } else { IDLE_G }, pr.branch), sty(if sel { WORK } else { FAINT }));
        let mut line = Line::from(vec![marker, name, branch]);
        if sel {
            line = line.style(Style::default().bg(INSET));
        }
        rows.push(line);
    }
    if rows.is_empty() {
        rows.push(Line::from(Span::styled(" no matches under ~/work ~/src ~/dev", sty(FAINT))));
    }
    f.render_widget(Paragraph::new(rows), chunks[2]);
}

fn centered(area: Rect, w: u16, h: u16) -> Rect {
    let x = area.x + (area.width.saturating_sub(w)) / 2;
    let y = area.y + (area.height.saturating_sub(h)) / 2;
    Rect { x, y, width: w.min(area.width), height: h.min(area.height) }
}
