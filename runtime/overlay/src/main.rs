#![cfg_attr(all(windows, not(debug_assertions)), windows_subsystem = "windows")]

use std::io::{self, BufRead};
use std::sync::atomic::{AtomicBool, Ordering};
use std::thread;
use std::time::{Duration, Instant};

use ab_glyph::{point, Font, FontRef, Glyph, PxScale, ScaleFont};
use crossbeam_channel::Sender;
use image::{ImageBuffer, Rgba};
use serde::Deserialize;
use windows::core::PCWSTR;
use windows::Win32::Foundation::{HWND, LPARAM, LRESULT, POINT, RECT, WPARAM};
use windows::Win32::Graphics::Gdi::{
    CreateCompatibleDC, CreateDIBSection, DeleteDC, DeleteObject, GetDC, ReleaseDC, SelectObject,
    AC_SRC_ALPHA, BITMAPINFO, BITMAPINFOHEADER, BI_RGB, BLENDFUNCTION, DIB_RGB_COLORS,
};
use windows::Win32::System::LibraryLoader::GetModuleHandleW;
use windows::Win32::UI::HiDpi::{
    SetProcessDpiAwarenessContext, DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2,
};
use windows::Win32::UI::WindowsAndMessaging::{
    CreateWindowExW, DefWindowProcW, DispatchMessageW, GetSystemMetrics, IsWindowVisible,
    PeekMessageW, PostQuitMessage, RegisterClassW, SetWindowPos, ShowWindow, TranslateMessage,
    UpdateLayeredWindow, MSG, PM_REMOVE,
    SM_CXVIRTUALSCREEN, SM_CYVIRTUALSCREEN, SM_XVIRTUALSCREEN, SM_YVIRTUALSCREEN, SW_HIDE,
    SW_SHOWNA, SWP_SHOWWINDOW, HWND_TOPMOST, ULW_ALPHA, WINDOW_EX_STYLE, WINDOW_STYLE, WM_DESTROY,
    WS_EX_LAYERED, WS_EX_TOOLWINDOW, WS_EX_TOPMOST, WS_EX_TRANSPARENT, WS_POPUP, WNDCLASSW,
};

mod vr;
use vr::{rgba_premultiplied, VrConfig, VrOverlay};

const VR_OVERLAY_KEY: &str = "sysaudio.subtitles";
const VR_OVERLAY_NAME: &str = "Audio Captions";

const DEFAULT_WIDTH: i32 = 1280;
const PAD_X: i32 = 24;
const PAD_Y: i32 = 16;
const LINE_GAP: i32 = 8;
const READING_PX: f32 = 26.0;
const SOURCE_PX: f32 = 38.0;
const ENGLISH_PX: f32 = 24.0;
const CAPTION_TTL: Duration = Duration::from_secs(10);

#[derive(Clone)]
struct Layout {
    width: i32,
    font_scale: f32,
    align: String,
    margin_bottom: i32,
    margin_x: i32,
    desktop_enabled: bool,
    vr_enabled: bool,
    vr: VrConfig,
}

impl Default for Layout {
    fn default() -> Self {
        Self {
            width: DEFAULT_WIDTH,
            font_scale: 1.0,
            align: "center".into(),
            margin_bottom: 72,
            margin_x: PAD_X,
            desktop_enabled: true,
            vr_enabled: false,
            vr: VrConfig::default(),
        }
    }
}

impl Layout {
    fn apply_ipc(&mut self, msg: &IpcMsg) {
        if let Some(v) = msg.width {
            self.width = v.clamp(480, 3840);
        }
        if let Some(v) = msg.font_scale {
            self.font_scale = v.clamp(0.5, 2.0);
        }
        if let Some(v) = msg.align.as_deref() {
            self.align = match v {
                "left" | "right" => v.to_string(),
                _ => "center".into(),
            };
        }
        if let Some(v) = msg.margin_bottom {
            self.margin_bottom = v.clamp(0, 800);
        }
        if let Some(v) = msg.margin_x {
            self.margin_x = v.clamp(0, 800);
        }
        if let Some(v) = msg.desktop_enabled {
            self.desktop_enabled = v;
        }
        if let Some(v) = msg.vr_enabled {
            self.vr_enabled = v;
        }
        if let Some(v) = msg.vr_width_m {
            self.vr.width_m = v.clamp(0.4, 4.0);
        }
        if let Some(v) = msg.vr_distance_m {
            self.vr.distance_m = v.clamp(0.5, 5.0);
        }
        if let Some(v) = msg.vr_y_offset_m {
            self.vr.y_offset_m = v.clamp(-1.5, 1.5);
        }
    }
}

static RUNNING: AtomicBool = AtomicBool::new(true);

#[derive(Debug, Deserialize)]
struct IpcMsg {
    op: String,
    #[serde(default)]
    req_id: Option<u64>,
    #[serde(default)]
    line_reading: String,
    #[serde(default)]
    line_source: String,
    #[serde(default)]
    line_english: String,
    #[serde(default)]
    width: Option<i32>,
    #[serde(default)]
    font_scale: Option<f32>,
    #[serde(default)]
    align: Option<String>,
    #[serde(default)]
    margin_bottom: Option<i32>,
    #[serde(default)]
    margin_x: Option<i32>,
    #[serde(default)]
    desktop_enabled: Option<bool>,
    #[serde(default)]
    vr_enabled: Option<bool>,
    #[serde(default)]
    vr_width_m: Option<f32>,
    #[serde(default)]
    vr_distance_m: Option<f32>,
    #[serde(default)]
    vr_y_offset_m: Option<f32>,
    #[serde(default)]
    screen_x: Option<i32>,
    #[serde(default)]
    screen_y: Option<i32>,
    #[serde(default)]
    screen_w: Option<i32>,
    #[serde(default)]
    screen_h: Option<i32>,
    #[serde(default)]
    labels: Vec<LabelMsg>,
}

#[derive(Debug, Clone, Deserialize)]
struct LabelMsg {
    #[serde(default)]
    text: String,
    #[serde(default)]
    roman: String,
    #[serde(default)]
    x: i32,
    #[serde(default)]
    y: i32,
    #[serde(default)]
    w: i32,
    #[serde(default)]
    h: i32,
    #[serde(default)]
    outline_only: bool,
}

enum Cmd {
    Caption {
        req_id: u64,
        deserialize_us: u64,
        t_recv: Instant,
        reading: String,
        source: String,
        english: String,
    },
    OcrLabels {
        req_id: u64,
        deserialize_us: u64,
        t_recv: Instant,
        region: (i32, i32, i32, i32),
        labels: Vec<LabelMsg>,
    },
    Configure(IpcMsg),
    Show,
    Hide,
    Quit,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum ContentKind {
    Strip,
    Labels,
}

struct State {
    line_reading: String,
    line_source: String,
    line_english: String,
    visible: bool,
    dirty: bool,
    expires_at: Option<Instant>,
    has_pending_timing: bool,
    pending_req_id: u64,
    pending_deserialize_us: u64,
    pending_queue_us: u64,
    kind: ContentKind,
    ocr_labels: Vec<LabelMsg>,
    region: (i32, i32, i32, i32),
}

impl Default for State {
    fn default() -> Self {
        Self {
            line_reading: String::new(),
            line_source: String::new(),
            line_english: String::new(),
            visible: false,
            dirty: true,
            expires_at: None,
            has_pending_timing: false,
            pending_req_id: 0,
            pending_deserialize_us: 0,
            pending_queue_us: 0,
            kind: ContentKind::Strip,
            ocr_labels: Vec::new(),
            region: (0, 0, 0, 0),
        }
    }
}

impl State {
    fn has_caption(&self) -> bool {
        !self.line_reading.trim().is_empty()
            || !self.line_source.trim().is_empty()
            || !self.line_english.trim().is_empty()
    }

    fn has_content(&self) -> bool {
        match self.kind {
            ContentKind::Strip => self.has_caption(),
            ContentKind::Labels => self.ocr_labels.iter().any(|l| {
                l.outline_only || !l.text.trim().is_empty()
            }),
        }
    }

    fn clear_caption(&mut self) {
        self.line_reading.clear();
        self.line_source.clear();
        self.line_english.clear();
        self.ocr_labels.clear();
        self.expires_at = None;
        self.visible = false;
        self.dirty = true;
    }

    fn tick_expiry(&mut self) {
        if let Some(until) = self.expires_at {
            if Instant::now() >= until {
                self.clear_caption();
            }
        }
    }
}

fn wide(s: &str) -> Vec<u16> {
    s.encode_utf16().chain(std::iter::once(0)).collect()
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Script {
    Hangul,
    Cjk,
    Latin,
}

fn char_script(c: char) -> Script {
    let o = c as u32;
    if (0xAC00..=0xD7AF).contains(&o) || (0x1100..=0x11FF).contains(&o) {
        Script::Hangul
    } else if (0x4E00..=0x9FFF).contains(&o)
        || (0x3400..=0x4DBF).contains(&o)
        || (0x3040..=0x30FF).contains(&o)
    {
        Script::Cjk
    } else {
        Script::Latin
    }
}

fn load_font_bytes(db: &fontdb::Database, families: &[&str]) -> Option<Vec<u8>> {
    for family in families {
        let id = db.query(&fontdb::Query {
            families: &[fontdb::Family::Name(family)],
            weight: fontdb::Weight::NORMAL,
            stretch: fontdb::Stretch::Normal,
            style: fontdb::Style::Normal,
        })?;
        let mut out: Option<Vec<u8>> = None;
        db.with_face_data(id, |data, _| {
            out = Some(data.to_vec());
        });
        if out.is_some() {
            return out;
        }
    }
    None
}

struct FontSet {
    cjk: FontRef<'static>,
    korean: FontRef<'static>,
    latin: FontRef<'static>,
    _storage: Vec<&'static [u8]>,
}

impl FontSet {
    fn load() -> Option<Self> {
        let mut db = fontdb::Database::new();
        db.load_system_fonts();
        let korean_bytes = load_font_bytes(
            &db,
            &["Malgun Gothic", "Malgun Gothic Semilight", "Gulim", "Dotum"],
        )?;
        let cjk_bytes = load_font_bytes(
            &db,
            &[
                "Microsoft YaHei",
                "Microsoft YaHei UI",
                "SimHei",
                "SimSun",
                "Meiryo",
                "Yu Gothic UI",
            ],
        )?;
        let latin_bytes = load_font_bytes(&db, &["Segoe UI", "Arial"])?;
        let leak = |b: Vec<u8>| -> &'static [u8] { Box::leak(b.into_boxed_slice()) };
        let korean_slice = leak(korean_bytes);
        let cjk_slice = leak(cjk_bytes);
        let latin_slice = leak(latin_bytes);
        Some(Self {
            cjk: FontRef::try_from_slice(cjk_slice).ok()?,
            korean: FontRef::try_from_slice(korean_slice).ok()?,
            latin: FontRef::try_from_slice(latin_slice).ok()?,
            _storage: vec![korean_slice, cjk_slice, latin_slice],
        })
    }

    fn for_char(&self, c: char) -> &FontRef<'static> {
        match char_script(c) {
            Script::Hangul => &self.korean,
            Script::Cjk => &self.cjk,
            Script::Latin => &self.latin,
        }
    }
}

fn text_width(fonts: &FontSet, scale: PxScale, text: &str) -> i32 {
    text.chars()
        .fold(0.0_f32, |w, c| {
            let font = fonts.for_char(c);
            let scaled = font.as_scaled(scale);
            w + scaled.h_advance(scaled.glyph_id(c))
        })
        .ceil() as i32
}

enum LineKind {
    Reading,
    Source,
    English,
}

struct DrawLine<'a> {
    text: &'a str,
    scale: PxScale,
    height: i32,
    kind: LineKind,
}

fn line_height(fonts: &FontSet, scale: PxScale, text: &str) -> i32 {
    (text
        .chars()
        .map(|c| fonts.for_char(c).as_scaled(scale).height())
        .fold(0.0_f32, f32::max)
        .ceil() as i32)
        .max(1)
}

#[derive(Default)]
struct StripTiming {
    layout_us: u64,
    render_us: u64,
}

fn render_strip(
    reading: &str,
    source: &str,
    english: &str,
    fonts: &FontSet,
    layout: &Layout,
    timing: &mut StripTiming,
) -> ImageBuffer<Rgba<u8>, Vec<u8>> {
    let t_layout = Instant::now();
    let width = layout.width.max(480);
    let scale_mul = layout.font_scale.max(0.5);
    let scale_r = PxScale::from(READING_PX * scale_mul);
    let scale_s = PxScale::from(SOURCE_PX * scale_mul);
    let scale_e = PxScale::from(ENGLISH_PX * scale_mul);
    let hr = line_height(fonts, scale_r, reading);
    let hs = line_height(fonts, scale_s, source);
    let he = line_height(fonts, scale_e, english);

    let mut lines: Vec<DrawLine> = Vec::new();
    if !reading.trim().is_empty() {
        lines.push(DrawLine {
            text: reading.trim(),
            scale: scale_r,
            height: hr,
            kind: LineKind::Reading,
        });
    }
    if !source.trim().is_empty() {
        lines.push(DrawLine {
            text: source.trim(),
            scale: scale_s,
            height: hs,
            kind: LineKind::Source,
        });
    }
    if !english.trim().is_empty() {
        lines.push(DrawLine {
            text: english.trim(),
            scale: scale_e,
            height: he,
            kind: LineKind::English,
        });
    }
    if lines.is_empty() {
        timing.layout_us = t_layout.elapsed().as_micros() as u64;
        return ImageBuffer::from_pixel(width as u32, 4, Rgba([0, 0, 0, 0]));
    }

    let total_h = PAD_Y * 2
        + lines.iter().map(|l| l.height).sum::<i32>()
        + LINE_GAP * (lines.len().saturating_sub(1) as i32);
    let h = total_h.max(40);
    let mut img = ImageBuffer::from_pixel(width as u32, h as u32, Rgba([12, 12, 16, 240]));
    timing.layout_us = t_layout.elapsed().as_micros() as u64;

    let t_render = Instant::now();
    let mut y = PAD_Y;
    for line in &lines {
        let (cr, cg, cb) = match line.kind {
            LineKind::Reading => (210, 210, 225),
            LineKind::Source => (255, 255, 255),
            LineKind::English => (200, 220, 255),
        };
        let tw = text_width(fonts, line.scale, line.text);
        let mut x = match layout.align.as_str() {
            "left" => layout.margin_x,
            "right" => (width - tw - layout.margin_x).max(layout.margin_x),
            _ => ((width - tw) / 2).max(layout.margin_x),
        };
        let baseline_y = y as f32
            + line
                .text
                .chars()
                .map(|c| fonts.for_char(c).as_scaled(line.scale).ascent())
                .fold(0.0_f32, f32::max);
        for ch in line.text.chars() {
            let font = fonts.for_char(ch);
            let scaled = font.as_scaled(line.scale);
            let gid = scaled.glyph_id(ch);
            let glyph = Glyph {
                id: gid,
                position: point(x as f32, baseline_y),
                scale: line.scale,
            };
            if let Some(outline) = scaled.outline_glyph(glyph) {
                let bounds = outline.px_bounds();
                let gw = bounds.width().ceil() as u32;
                let gh = bounds.height().ceil() as u32;
                if gw > 0 && gh > 0 {
                    let mut glyph_img = ImageBuffer::from_pixel(gw, gh, Rgba([0, 0, 0, 0]));
                    outline.draw(|gx, gy, a| {
                        let alpha = (a * 255.0).round() as u8;
                        if alpha > 0 {
                            let px = glyph_img.get_pixel_mut(gx, gy);
                            *px = Rgba([cr, cg, cb, alpha]);
                        }
                    });
                    let ox = bounds.min.x as i32;
                    let oy = bounds.min.y as i32;
                    for gy in 0..gh {
                        for gx in 0..gw {
                            let p = glyph_img.get_pixel(gx, gy);
                            if p[3] == 0 {
                                continue;
                            }
                            let dx = ox + gx as i32;
                            let dy = oy + gy as i32;
                            if dx >= 0 && dy >= 0 && dx < width && dy < h {
                                let bg = img.get_pixel(dx as u32, dy as u32);
                                let a = p[3] as f32 / 255.0;
                                let r = (bg[0] as f32 * (1.0 - a) + p[0] as f32 * a) as u8;
                                let g = (bg[1] as f32 * (1.0 - a) + p[1] as f32 * a) as u8;
                                let b = (bg[2] as f32 * (1.0 - a) + p[2] as f32 * a) as u8;
                                img.put_pixel(dx as u32, dy as u32, Rgba([r, g, b, 255]));
                            }
                        }
                    }
                }
            }
            x += scaled.h_advance(gid).ceil() as i32;
        }
        y += line.height + LINE_GAP;
    }
    timing.render_us = t_render.elapsed().as_micros() as u64;
    img
}

fn rgba_to_bgra(img: &ImageBuffer<Rgba<u8>, Vec<u8>>) -> (Vec<u8>, i32, i32) {
    let (w, h) = (img.width() as i32, img.height() as i32);
    let mut out = Vec::with_capacity((w * h * 4) as usize);
    for p in img.pixels() {
        out.push(p[2]);
        out.push(p[1]);
        out.push(p[0]);
        out.push(p[3]);
    }
    (out, w, h)
}


const LABEL_FONT_MAX_PX: f32 = 64.0;
const ROMAN_FONT_MAX_PX: f32 = 17.0;

fn render_labels(
    region_w: i32,
    region_h: i32,
    labels: &[LabelMsg],
    fonts: &FontSet,
) -> ImageBuffer<Rgba<u8>, Vec<u8>> {
    let w = region_w.max(1) as u32;
    let h = region_h.max(1) as u32;
    let mut img = ImageBuffer::from_pixel(w, h, Rgba([0, 0, 0, 0]));
    for lab in labels {
        draw_label_in_box(&mut img, region_w, region_h, lab, fonts);
    }
    img
}

fn draw_label_in_box(
    img: &mut ImageBuffer<Rgba<u8>, Vec<u8>>,
    region_w: i32,
    region_h: i32,
    lab: &LabelMsg,
    fonts: &FontSet,
) {
    const PAD: i32 = 3;
    let x0 = (lab.x - PAD).clamp(0, region_w);
    let y0 = (lab.y - PAD).clamp(0, region_h);
    let x1 = (lab.x + lab.w + PAD).clamp(0, region_w);
    let y1 = (lab.y + lab.h + PAD).clamp(0, region_h);
    let inner_w = x1 - x0;
    let inner_h = y1 - y0;
    if inner_w < 8 || inner_h < 8 {
        return;
    }

    let text = lab.text.trim();
    if text.is_empty() {
        if lab.outline_only {
            draw_rect_outline(img, x0, y0, x1, y1, (0, 255, 0, 255), 2);
        }
        return;
    }

    fill_rect(img, x0, y0, x1, y1, Rgba([255, 244, 232, 255]));
    draw_rect_outline(img, x0, y0, x1, y1, (0, 200, 0, 255), 2);

    let roman = lab.roman.trim();
    if !roman.is_empty() {
        let roman_band = (inner_h * 38 / 100).max(14).min(inner_h - 10);
        if roman_band >= 12 {
            let roman_bottom = y0 + roman_band;
            fill_rect(img, x0, y0, x1, roman_bottom, Rgba([240, 220, 200, 255]));
            draw_hline(img, x0 + 1, roman_bottom, x1 - 1, Rgba([187, 187, 187, 255]));
            draw_text_block(
                img, fonts, roman, x0 + 2, y0 + 1, x1 - 2, roman_bottom - 1,
                ROMAN_FONT_MAX_PX, (24, 32, 48), false,
            );
            draw_text_block(
                img, fonts, text, x0 + 2, roman_bottom + 2, x1 - 2, y1 - 2,
                LABEL_FONT_MAX_PX, (16, 16, 16), true,
            );
            return;
        }
    }
    draw_text_block(
        img, fonts, text, x0 + 2, y0 + 2, x1 - 2, y1 - 2,
        LABEL_FONT_MAX_PX, (16, 16, 16), true,
    );
}

fn draw_text_block(
    img: &mut ImageBuffer<Rgba<u8>, Vec<u8>>,
    fonts: &FontSet,
    text: &str,
    bx0: i32,
    by0: i32,
    bx1: i32,
    by1: i32,
    max_px: f32,
    color: (u8, u8, u8),
    shadow: bool,
) {
    let box_w = bx1 - bx0;
    let box_h = by1 - by0;
    if box_w < 4 || box_h < 4 {
        return;
    }
    let (px, lines) = fit_scale(fonts, text, box_w, box_h, max_px);
    let scale = PxScale::from(px);
    let lh = line_advance(fonts, scale);
    let total_h = lines.len() as f32 * lh;
    let ascent = fonts.latin.as_scaled(scale).ascent();
    let mut y = by0 as f32 + ((box_h as f32 - total_h) / 2.0).max(0.0);
    for line in &lines {
        let lw = text_width(fonts, scale, line);
        let x = bx0 + ((box_w - lw) / 2).max(0);
        let baseline = y + ascent;
        if shadow {
            for (ox, oy) in [(1, 0), (-1, 0), (0, 1), (0, -1)] {
                draw_line(img, fonts, scale, x + ox, baseline + oy as f32, line, (0, 0, 0));
            }
        }
        draw_line(img, fonts, scale, x, baseline, line, color);
        y += lh;
    }
}

fn fit_scale(
    fonts: &FontSet,
    text: &str,
    box_w: i32,
    box_h: i32,
    max_px: f32,
) -> (f32, Vec<String>) {
    let mut lo = 8.0_f32;
    let mut hi = max_px.max(8.0);
    let mut best = 8.0_f32;
    let mut best_lines = wrap_lines(fonts, PxScale::from(8.0), text, box_w);
    for _ in 0..8 {
        if lo > hi {
            break;
        }
        let mid = ((lo + hi) / 2.0).floor();
        let scale = PxScale::from(mid);
        let lines = wrap_lines(fonts, scale, text, box_w);
        let total_h = (lines.len() as f32 * line_advance(fonts, scale)).ceil() as i32;
        let max_lw = lines
            .iter()
            .map(|l| text_width(fonts, scale, l))
            .max()
            .unwrap_or(0);
        if total_h <= box_h && max_lw <= box_w {
            best = mid;
            best_lines = lines;
            lo = mid + 1.0;
        } else {
            hi = mid - 1.0;
        }
    }
    (best, best_lines)
}

fn wrap_lines(fonts: &FontSet, scale: PxScale, text: &str, max_w: i32) -> Vec<String> {
    let max_w = max_w.max(1);
    let mut lines: Vec<String> = Vec::new();
    if text.contains(' ') {
        let space_w = text_width(fonts, scale, " ");
        let mut cur = String::new();
        let mut cur_w = 0;
        for word in text.split_whitespace() {
            let ww = text_width(fonts, scale, word);
            if cur.is_empty() {
                cur.push_str(word);
                cur_w = ww;
            } else if cur_w + space_w + ww <= max_w {
                cur.push(' ');
                cur.push_str(word);
                cur_w += space_w + ww;
            } else {
                lines.push(std::mem::take(&mut cur));
                cur.push_str(word);
                cur_w = ww;
            }
        }
        if !cur.is_empty() {
            lines.push(cur);
        }
    } else {
        let mut cur = String::new();
        let mut cur_w = 0;
        for ch in text.chars() {
            let cw = text_width(fonts, scale, ch.encode_utf8(&mut [0u8; 4]));
            if !cur.is_empty() && cur_w + cw > max_w {
                lines.push(std::mem::take(&mut cur));
                cur_w = 0;
            }
            cur.push(ch);
            cur_w += cw;
        }
        if !cur.is_empty() {
            lines.push(cur);
        }
    }
    if lines.is_empty() {
        lines.push(String::new());
    }
    lines
}

fn line_advance(fonts: &FontSet, scale: PxScale) -> f32 {
    fonts.latin.as_scaled(scale).height().max(1.0)
}

fn draw_line(
    img: &mut ImageBuffer<Rgba<u8>, Vec<u8>>,
    fonts: &FontSet,
    scale: PxScale,
    x_start: i32,
    baseline_y: f32,
    text: &str,
    color: (u8, u8, u8),
) {
    let clip_w = img.width() as i32;
    let clip_h = img.height() as i32;
    let (cr, cg, cb) = color;
    let mut x = x_start;
    for ch in text.chars() {
        let font = fonts.for_char(ch);
        let scaled = font.as_scaled(scale);
        let gid = scaled.glyph_id(ch);
        let glyph = Glyph {
            id: gid,
            position: point(x as f32, baseline_y),
            scale,
        };
        if let Some(outline) = scaled.outline_glyph(glyph) {
            let bounds = outline.px_bounds();
            let ox = bounds.min.x as i32;
            let oy = bounds.min.y as i32;
            outline.draw(|gx, gy, a| {
                let alpha = (a * 255.0).round() as u8;
                if alpha == 0 {
                    return;
                }
                let dx = ox + gx as i32;
                let dy = oy + gy as i32;
                if dx < 0 || dy < 0 || dx >= clip_w || dy >= clip_h {
                    return;
                }
                let bg = *img.get_pixel(dx as u32, dy as u32);
                let af = alpha as f32 / 255.0;
                let mix = |s: u8, d: u8| (d as f32 * (1.0 - af) + s as f32 * af).round() as u8;
                img.put_pixel(
                    dx as u32,
                    dy as u32,
                    Rgba([mix(cr, bg[0]), mix(cg, bg[1]), mix(cb, bg[2]), alpha.max(bg[3])]),
                );
            });
        }
        x += scaled.h_advance(gid).ceil() as i32;
    }
}

fn fill_rect(img: &mut ImageBuffer<Rgba<u8>, Vec<u8>>, x0: i32, y0: i32, x1: i32, y1: i32, c: Rgba<u8>) {
    let w = img.width() as i32;
    let h = img.height() as i32;
    for y in y0.max(0)..y1.min(h) {
        for x in x0.max(0)..x1.min(w) {
            img.put_pixel(x as u32, y as u32, c);
        }
    }
}

fn draw_rect_outline(
    img: &mut ImageBuffer<Rgba<u8>, Vec<u8>>,
    x0: i32,
    y0: i32,
    x1: i32,
    y1: i32,
    c: (u8, u8, u8, u8),
    thickness: i32,
) {
    let col = Rgba([c.0, c.1, c.2, c.3]);
    let t = thickness.max(1);
    fill_rect(img, x0, y0, x1, y0 + t, col);
    fill_rect(img, x0, y1 - t, x1, y1, col);
    fill_rect(img, x0, y0, x0 + t, y1, col);
    fill_rect(img, x1 - t, y0, x1, y1, col);
}

fn draw_hline(img: &mut ImageBuffer<Rgba<u8>, Vec<u8>>, x0: i32, y: i32, x1: i32, c: Rgba<u8>) {
    fill_rect(img, x0, y, x1, y + 1, c);
}

struct OverlayWin {
    hwnd: HWND,
    fonts: FontSet,
    layout: Layout,
}

impl OverlayWin {
    fn screen_work_area() -> RECT {
        unsafe {
            RECT {
                left: GetSystemMetrics(SM_XVIRTUALSCREEN),
                top: GetSystemMetrics(SM_YVIRTUALSCREEN),
                right: GetSystemMetrics(SM_XVIRTUALSCREEN) + GetSystemMetrics(SM_CXVIRTUALSCREEN),
                bottom: GetSystemMetrics(SM_YVIRTUALSCREEN) + GetSystemMetrics(SM_CYVIRTUALSCREEN),
            }
        }
    }

    fn present(&self, img: &ImageBuffer<Rgba<u8>, Vec<u8>>, origin: Option<(i32, i32)>) -> bool {
        let (bgra, w, h) = rgba_to_bgra(img);
        if w < 1 || h < 1 {
            return false;
        }
        unsafe {
            let (x, y) = match origin {
                Some((ox, oy)) => (ox, oy),
                None => {
                    let work = Self::screen_work_area();
                    let x = match self.layout.align.as_str() {
                        "left" => work.left + self.layout.margin_x,
                        "right" => work.right - w - self.layout.margin_x,
                        _ => work.left + ((work.right - work.left - w) / 2).max(0),
                    };
                    let y = (work.bottom - h - self.layout.margin_bottom).max(work.top);
                    (x, y)
                }
            };

            let screen_dc = GetDC(None);
            if screen_dc.is_invalid() {
                return false;
            }
            let mem_dc = CreateCompatibleDC(screen_dc);
            if mem_dc.is_invalid() {
                let _ = ReleaseDC(None, screen_dc);
                return false;
            }

            let bmi = BITMAPINFO {
                bmiHeader: BITMAPINFOHEADER {
                    biSize: std::mem::size_of::<BITMAPINFOHEADER>() as u32,
                    biWidth: w,
                    biHeight: -h,
                    biPlanes: 1,
                    biBitCount: 32,
                    biCompression: BI_RGB.0,
                    ..Default::default()
                },
                ..Default::default()
            };
            let mut bits = std::ptr::null_mut();
            let hbmp = match CreateDIBSection(mem_dc, &bmi, DIB_RGB_COLORS, &mut bits, None, 0) {
                Ok(h) => h,
                Err(_) => {
                    let _ = DeleteDC(mem_dc);
                    let _ = ReleaseDC(None, screen_dc);
                    return false;
                }
            };
            if bits.is_null() {
                let _ = DeleteObject(hbmp);
                let _ = DeleteDC(mem_dc);
                let _ = ReleaseDC(None, screen_dc);
                return false;
            }
            let old = SelectObject(mem_dc, hbmp);
            std::ptr::copy_nonoverlapping(bgra.as_ptr(), bits as *mut u8, bgra.len());

            let pt_dst = POINT { x, y };
            let size = windows::Win32::Foundation::SIZE { cx: w, cy: h };
            let pt_src = POINT { x: 0, y: 0 };
            let blend = BLENDFUNCTION {
                BlendOp: 0,
                BlendFlags: 0,
                SourceConstantAlpha: 255,
                AlphaFormat: AC_SRC_ALPHA as u8,
            };

            let ok = UpdateLayeredWindow(
                self.hwnd,
                screen_dc,
                Some(&pt_dst),
                Some(&size),
                mem_dc,
                Some(&pt_src),
                windows::Win32::Foundation::COLORREF(0),
                Some(&blend),
                ULW_ALPHA,
            )
            .is_ok();

            let _ = SelectObject(mem_dc, old);
            let _ = DeleteObject(hbmp);
            let _ = DeleteDC(mem_dc);
            let _ = ReleaseDC(None, screen_dc);

            if ok {
                let _ = SetWindowPos(
                    self.hwnd,
                    HWND_TOPMOST,
                    x,
                    y,
                    w,
                    h,
                    SWP_SHOWWINDOW,
                );
                let _ = ShowWindow(self.hwnd, SW_SHOWNA);
            }
            ok
        }
    }

    fn hide_window(&self) {
        unsafe {
            let _ = ShowWindow(self.hwnd, SW_HIDE);
        }
    }

    fn render_current(
        &self,
        state: &State,
        timing: &mut StripTiming,
    ) -> Option<ImageBuffer<Rgba<u8>, Vec<u8>>> {
        if !state.visible || !state.has_content() {
            return None;
        }
        match state.kind {
            ContentKind::Strip => Some(render_strip(
                &state.line_reading,
                &state.line_source,
                &state.line_english,
                &self.fonts,
                &self.layout,
                timing,
            )),
            ContentKind::Labels => {
                let t = Instant::now();
                let img =
                    render_labels(state.region.2, state.region.3, &state.ocr_labels, &self.fonts);
                timing.render_us = t.elapsed().as_micros() as u64;
                Some(img)
            }
        }
    }
}

fn create_overlay_window() -> Option<OverlayWin> {
    let fonts = FontSet::load()?;

    unsafe {
        let class_name = wide("SysAudioSubtitleWin32");
        let hinst = GetModuleHandleW(None).ok()?;
        let wc = WNDCLASSW {
            lpfnWndProc: Some(wnd_proc),
            hInstance: hinst.into(),
            lpszClassName: PCWSTR(class_name.as_ptr()),
            ..Default::default()
        };
        let _ = RegisterClassW(&wc);

        let ex = WINDOW_EX_STYLE(
            (WS_EX_LAYERED | WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_TRANSPARENT).0,
        );
        let hwnd = CreateWindowExW(
            ex,
            PCWSTR(class_name.as_ptr()),
            PCWSTR(wide("System Audio Subtitles").as_ptr()),
            WINDOW_STYLE(WS_POPUP.0),
            0,
            0,
            DEFAULT_WIDTH,
            80,
            None,
            None,
            hinst,
            None,
        )
        .ok()?;

        Some(OverlayWin {
            hwnd,
            fonts,
            layout: Layout::default(),
        })
    }
}

unsafe extern "system" fn wnd_proc(
    hwnd: HWND,
    msg: u32,
    wparam: WPARAM,
    lparam: LPARAM,
) -> LRESULT {
    match msg {
        WM_DESTROY => {
            RUNNING.store(false, Ordering::SeqCst);
            let _ = PostQuitMessage(0);
            LRESULT(0)
        }
        _ => DefWindowProcW(hwnd, msg, wparam, lparam),
    }
}

fn stdin_thread(tx: Sender<Cmd>) {
    let stdin = io::stdin();
    for line in stdin.lock().lines() {
        let Ok(line) = line else { break };
        if line.trim().is_empty() {
            continue;
        }
        let t_parse = Instant::now();
        let msg: IpcMsg = match serde_json::from_str(&line) {
            Ok(m) => m,
            Err(e) => {
                eprintln!("overlay: bad json: {e}");
                continue;
            }
        };
        let deserialize_us = t_parse.elapsed().as_micros() as u64;
        let t_recv = Instant::now();
        let cmd = match msg.op.as_str() {
            "caption" => Cmd::Caption {
                req_id: msg.req_id.unwrap_or(0),
                deserialize_us,
                t_recv,
                reading: msg.line_reading,
                source: msg.line_source,
                english: msg.line_english,
            },
            "ocr_labels" => Cmd::OcrLabels {
                req_id: msg.req_id.unwrap_or(0),
                deserialize_us,
                t_recv,
                region: (
                    msg.screen_x.unwrap_or(0),
                    msg.screen_y.unwrap_or(0),
                    msg.screen_w.unwrap_or(DEFAULT_WIDTH),
                    msg.screen_h.unwrap_or(0),
                ),
                labels: msg.labels,
            },
            "configure" => Cmd::Configure(msg),
            "show" => Cmd::Show,
            "hide" => Cmd::Hide,
            "quit" => Cmd::Quit,
            _ => continue,
        };
        if tx.send(cmd).is_err() {
            break;
        }
    }
    let _ = tx.send(Cmd::Quit);
}

fn emit_vr_status(ok: bool, detail: &str) {
    use std::io::Write;
    let safe = detail.replace('\\', "\\\\").replace('"', "\\\"");
    let mut out = std::io::stdout().lock();
    let _ = writeln!(out, "{{\"event\":\"vr_status\",\"ok\":{ok},\"detail\":\"{safe}\"}}");
    let _ = out.flush();
}

fn emit_caption_timing(
    req_id: u64,
    deserialize_us: u64,
    queue_us: u64,
    layout_us: u64,
    render_us: u64,
    present_us: u64,
) {
    use std::io::Write;
    let total = deserialize_us + queue_us + layout_us + render_us + present_us;
    let mut out = std::io::stdout().lock();
    let _ = writeln!(
        out,
        "{{\"event\":\"caption_timing\",\"req_id\":{req_id},\"spans_us\":{{\"deserialize\":{deserialize_us},\"queue\":{queue_us},\"layout\":{layout_us},\"render\":{render_us},\"present\":{present_us}}},\"total_us\":{total}}}"
    );
    let _ = out.flush();
}

// Must run before any window is created. On displays scaled above 100%, without
// per-monitor DPI awareness Windows virtualizes our SetWindowPos coordinates and
// DWM bitmap-scales the layered window, so the overlay won't share the target
// window's physical-pixel coordinate space and the captions land misaligned.
fn enable_dpi_awareness_if_requested() {
    if std::env::var_os("TO_OVERLAY_DPI_AWARE").is_none() {
        return;
    }
    unsafe {
        let _ = SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    }
}

fn main() {
    enable_dpi_awareness_if_requested();
    let Some(mut overlay) = create_overlay_window() else {
        eprintln!("overlay: failed to create window (font/window)");
        std::process::exit(1);
    };

    let (tx, rx) = crossbeam_channel::unbounded();
    thread::spawn(move || stdin_thread(tx));

    let mut state = State::default();
    let vr_key = std::env::var("TO_OVERLAY_VR_KEY").unwrap_or_else(|_| VR_OVERLAY_KEY.to_string());
    let vr_name =
        std::env::var("TO_OVERLAY_VR_NAME").unwrap_or_else(|_| VR_OVERLAY_NAME.to_string());
    let mut vr = VrOverlay::new(&vr_key, &vr_name);
    let mut next_vr_check = Instant::now();
    let mut last_vr_status: Option<bool> = None;

    overlay.hide_window();

    unsafe {
        let mut msg = MSG::default();
        while RUNNING.load(Ordering::SeqCst) {
            state.tick_expiry();
            while let Ok(cmd) = rx.try_recv() {
                match cmd {
                    Cmd::Caption {
                        req_id,
                        deserialize_us,
                        t_recv,
                        reading,
                        source,
                        english,
                    } => {
                        state.kind = ContentKind::Strip;
                        state.line_reading = reading;
                        state.line_source = source;
                        state.line_english = english;
                        if state.has_caption() {
                            state.visible = true;
                            state.expires_at = Some(Instant::now() + CAPTION_TTL);
                        } else {
                            state.clear_caption();
                        }
                        state.dirty = true;
                        if req_id != 0 {
                            state.pending_req_id = req_id;
                            state.pending_deserialize_us = deserialize_us;
                            state.pending_queue_us = t_recv.elapsed().as_micros() as u64;
                            state.has_pending_timing = true;
                        }
                    }
                    Cmd::OcrLabels {
                        req_id,
                        deserialize_us,
                        t_recv,
                        region,
                        labels,
                    } => {
                        state.kind = ContentKind::Labels;
                        state.region = region;
                        state.ocr_labels = labels;
                        if state.has_content() {
                            state.visible = true;
                            state.expires_at = None;
                        } else {
                            state.clear_caption();
                        }
                        state.dirty = true;
                        if req_id != 0 {
                            state.pending_req_id = req_id;
                            state.pending_deserialize_us = deserialize_us;
                            state.pending_queue_us = t_recv.elapsed().as_micros() as u64;
                            state.has_pending_timing = true;
                        }
                    }
                    Cmd::Configure(msg) => {
                        overlay.layout.apply_ipc(&msg);
                        next_vr_check = Instant::now();
                        state.dirty = true;
                    }
                    Cmd::Show => {
                        state.visible = true;
                        state.dirty = true;
                    }
                    Cmd::Hide => {
                        state.clear_caption();
                    }
                    Cmd::Quit => RUNNING.store(false, Ordering::SeqCst),
                }
            }

            if Instant::now() >= next_vr_check {
                next_vr_check = Instant::now() + Duration::from_secs(2);
                if overlay.layout.vr_enabled {
                    if !vr.is_ready() {
                        match vr.ensure() {
                            Ok(()) => {
                                if last_vr_status != Some(true) {
                                    emit_vr_status(true, "ready");
                                    last_vr_status = Some(true);
                                }
                                state.dirty = true;
                            }
                            Err(detail) => {
                                if last_vr_status != Some(false) {
                                    emit_vr_status(false, &detail);
                                    last_vr_status = Some(false);
                                }
                            }
                        }
                    }
                } else if vr.is_ready() {
                    vr.hide();
                }
            }

            if vr.is_ready() && vr.pump_events() {
                if last_vr_status != Some(false) {
                    emit_vr_status(false, "SteamVR is shutting down");
                    last_vr_status = Some(false);
                }
                next_vr_check = Instant::now();
            }

            if state.dirty {
                let mut strip_timing = StripTiming::default();
                let img = overlay.render_current(&state, &mut strip_timing);
                let origin = match state.kind {
                    ContentKind::Labels => Some((state.region.0, state.region.1)),
                    ContentKind::Strip => None,
                };

                let t_present = Instant::now();
                if overlay.layout.desktop_enabled {
                    match &img {
                        Some(im) => {
                            overlay.present(im, origin);
                        }
                        None => overlay.hide_window(),
                    }
                } else {
                    overlay.hide_window();
                }

                if overlay.layout.vr_enabled && vr.is_ready() {
                    match &img {
                        Some(im) => {
                            let (buf, w, h) = rgba_premultiplied(im);
                            vr.present(&buf, w, h, &overlay.layout.vr);
                        }
                        None => vr.hide(),
                    }
                }
                let present_us = t_present.elapsed().as_micros() as u64;

                if state.has_pending_timing {
                    emit_caption_timing(
                        state.pending_req_id,
                        state.pending_deserialize_us,
                        state.pending_queue_us,
                        strip_timing.layout_us,
                        strip_timing.render_us,
                        present_us,
                    );
                    state.has_pending_timing = false;
                }

                state.dirty = false;
            }

            while PeekMessageW(&mut msg, None, 0, 0, PM_REMOVE).into() {
                if msg.message == WM_DESTROY {
                    RUNNING.store(false, Ordering::SeqCst);
                    break;
                }
                let _ = TranslateMessage(&msg);
                let _ = DispatchMessageW(&msg);
            }
            if !RUNNING.load(Ordering::SeqCst) {
                break;
            }
            thread::sleep(Duration::from_millis(16));
        }
        let visible = IsWindowVisible(overlay.hwnd).as_bool();
        eprintln!("overlay exit visible={visible}");
    }
}
