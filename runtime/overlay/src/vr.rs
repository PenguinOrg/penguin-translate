//! OpenVR overlay output. OpenVR wants premultiplied-RGBA bytes (no channel swap,
//! unlike the Win32 GDI path which wants BGRA).

use std::ffi::CString;
use std::os::raw::{c_char, c_void};

use image::{ImageBuffer, Rgba};

extern "C" {
    fn wt_vr_init() -> i32;
    fn wt_vr_shutdown();
    fn wt_vr_pump_events(handle: u64) -> i32;
    fn wt_vr_overlay_create(key: *const c_char, name: *const c_char) -> u64;
    fn wt_vr_overlay_destroy(handle: u64);
    fn wt_vr_overlay_set_raw(handle: u64, rgba: *const c_void, width: u32, height: u32) -> i32;
    fn wt_vr_overlay_show(handle: u64) -> i32;
    fn wt_vr_overlay_hide(handle: u64) -> i32;
    fn wt_vr_overlay_set_transform_hmd_offset(
        handle: u64,
        distance_m: f32,
        width_m: f32,
        y_offset_m: f32,
    ) -> i32;
}

#[derive(Clone, Copy)]
pub struct VrConfig {
    pub width_m: f32,
    pub distance_m: f32,
    pub y_offset_m: f32,
}

impl Default for VrConfig {
    fn default() -> Self {
        Self {
            width_m: 1.6,
            distance_m: 1.8,
            y_offset_m: 0.0,
        }
    }
}

pub struct VrOverlay {
    key: String,
    name: String,
    handle: u64,
    ready: bool,
}

impl VrOverlay {
    pub fn new(key: &str, name: &str) -> Self {
        Self {
            key: key.to_string(),
            name: name.to_string(),
            handle: 0,
            ready: false,
        }
    }

    pub fn is_ready(&self) -> bool {
        self.ready && self.handle != 0
    }

    pub fn ensure(&mut self) -> Result<(), String> {
        if self.is_ready() {
            return Ok(());
        }
        let code = unsafe { wt_vr_init() };
        if code != 0 {
            return Err(format!(
                "SteamVR init failed (code {code}) — is SteamVR running?"
            ));
        }
        if self.handle == 0 {
            let key = CString::new(self.key.as_str()).map_err(|e| e.to_string())?;
            let name = CString::new(self.name.as_str()).map_err(|e| e.to_string())?;
            let h = unsafe { wt_vr_overlay_create(key.as_ptr(), name.as_ptr()) };
            if h == 0 {
                return Err("SteamVR overlay create failed".into());
            }
            self.handle = h;
        }
        self.ready = true;
        Ok(())
    }

    pub fn present(&self, rgba_premul: &[u8], w: u32, h: u32, cfg: &VrConfig) -> bool {
        if self.handle == 0 || w == 0 || h == 0 {
            return false;
        }
        if rgba_premul.len() < (w as usize) * (h as usize) * 4 {
            return false;
        }
        unsafe {
            if wt_vr_overlay_set_raw(self.handle, rgba_premul.as_ptr() as *const c_void, w, h) == 0 {
                return false;
            }
            if wt_vr_overlay_set_transform_hmd_offset(
                self.handle,
                cfg.distance_m,
                cfg.width_m,
                cfg.y_offset_m,
            ) == 0
            {
                return false;
            }
            wt_vr_overlay_show(self.handle) != 0
        }
    }

    // Must be called regularly while connected: an overlay app that never drains
    // SteamVR's event queue is treated as unresponsive and freezes on its last frame.
    pub fn pump_events(&mut self) -> bool {
        if self.handle == 0 {
            return false;
        }
        if unsafe { wt_vr_pump_events(self.handle) } != 0 {
            self.destroy();
            unsafe {
                wt_vr_shutdown();
            }
            return true;
        }
        false
    }

    pub fn hide(&self) {
        if self.handle != 0 {
            unsafe {
                wt_vr_overlay_hide(self.handle);
            }
        }
    }

    pub fn destroy(&mut self) {
        if self.handle != 0 {
            unsafe {
                wt_vr_overlay_destroy(self.handle);
            }
            self.handle = 0;
        }
        self.ready = false;
    }
}

impl Drop for VrOverlay {
    fn drop(&mut self) {
        self.destroy();
        unsafe {
            wt_vr_shutdown();
        }
    }
}

// Straight-alpha RGBA -> tightly packed premultiplied-RGBA, as OpenVR expects.
pub fn rgba_premultiplied(img: &ImageBuffer<Rgba<u8>, Vec<u8>>) -> (Vec<u8>, u32, u32) {
    let (w, h) = (img.width(), img.height());
    let mut out = Vec::with_capacity((w as usize) * (h as usize) * 4);
    for p in img.pixels() {
        let a = p[3] as u32;
        let pm = |c: u8| ((c as u32 * a) / 255) as u8;
        out.push(pm(p[0]));
        out.push(pm(p[1]));
        out.push(pm(p[2]));
        out.push(p[3]);
    }
    (out, w, h)
}
