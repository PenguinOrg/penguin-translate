#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <string.h>
#include <stdio.h>

#include "openvr_capi.h"
#include "openvr_shim.h"

// OpenVR's k_unMaxTrackedDeviceCount is a `static const` (== 64), which is not a
// compile-time constant in C — gcc accepted `poses[k_unMaxTrackedDeviceCount]`
// as a VLA, but MSVC (used by the Rust `cc` build) has no C VLAs. Use a fixed
// macro so the same shim compiles under both toolchains.
#define WT_MAX_TRACKED_DEVICES 64

static HMODULE g_dll;
static struct VR_IVROverlay_FnTable *g_overlay;
static struct VR_IVRSystem_FnTable *g_system;

typedef intptr_t (*fn_VR_InitInternal)(EVRInitError *, EVRApplicationType);
typedef void (*fn_VR_ShutdownInternal)(void);
typedef intptr_t (*fn_VR_GetGenericInterface)(const char *, EVRInitError *);
typedef bool (*fn_VR_IsHmdPresent)(void);
typedef bool (*fn_VR_IsRuntimeInstalled)(void);

static fn_VR_InitInternal pVR_InitInternal;
static fn_VR_ShutdownInternal pVR_ShutdownInternal;
static fn_VR_GetGenericInterface pVR_GetGenericInterface;
static fn_VR_IsHmdPresent pVR_IsHmdPresent;
static fn_VR_IsRuntimeInstalled pVR_IsRuntimeInstalled;

static int try_load_dll_path(const char *path) {
    if (!path || !path[0]) return 0;
    g_dll = LoadLibraryA(path);
    return g_dll != NULL;
}

static int load_openvr_dll(void) {
    if (g_dll) return 1;

    char path[MAX_PATH];
    const char *candidates[] = {
        "openvr_api.dll",
        "third_party\\openvr\\lib\\win64\\openvr_api.dll",
        NULL
    };

    for (int i = 0; candidates[i]; i++) {
        if (try_load_dll_path(candidates[i])) return 1;
    }

    const char *steam_paths[] = {
        "C:\\Program Files (x86)\\Steam\\steamapps\\common\\SteamVR\\bin\\win64\\openvr_api.dll",
        "C:\\Program Files\\Steam\\steamapps\\common\\SteamVR\\bin\\win64\\openvr_api.dll",
    };
    for (int i = 0; i < 2; i++) {
        if (try_load_dll_path(steam_paths[i])) return 1;
    }

    DWORD n = GetEnvironmentVariableA("VR_RUNTIME_PATH", path, MAX_PATH);
    if (n > 0 && n < MAX_PATH) {
        char full[MAX_PATH];
        snprintf(full, sizeof(full), "%s\\bin\\win64\\openvr_api.dll", path);
        if (try_load_dll_path(full)) return 1;
    }

    return 0;
}

static int resolve_exports(void) {
    pVR_InitInternal = (fn_VR_InitInternal)GetProcAddress(g_dll, "VR_InitInternal");
    pVR_ShutdownInternal = (fn_VR_ShutdownInternal)GetProcAddress(g_dll, "VR_ShutdownInternal");
    pVR_GetGenericInterface = (fn_VR_GetGenericInterface)GetProcAddress(g_dll, "VR_GetGenericInterface");
    pVR_IsHmdPresent = (fn_VR_IsHmdPresent)GetProcAddress(g_dll, "VR_IsHmdPresent");
    pVR_IsRuntimeInstalled = (fn_VR_IsRuntimeInstalled)GetProcAddress(g_dll, "VR_IsRuntimeInstalled");
    return pVR_InitInternal && pVR_ShutdownInternal && pVR_GetGenericInterface;
}

int wt_vr_init(void) {
    if (g_overlay && g_system) return 0;
    if (!load_openvr_dll()) return -1;
    if (!resolve_exports()) return -2;

    // Crash-safe pre-checks. Calling VR_InitInternal / the overlay FnTable when
    // no runtime or headset is actually available faults inside openvr_api.dll
    // and an access violation in C code cannot be recovered by Go — it kills the
    // whole process. VR_IsRuntimeInstalled / VR_IsHmdPresent are explicitly safe
    // to call before init and without a running SteamVR server, so use them to
    // bail out cleanly (non-zero -> Presenter records initErr, no crash).
    if (pVR_IsRuntimeInstalled && !pVR_IsRuntimeInstalled()) return -3;
    if (pVR_IsHmdPresent && !pVR_IsHmdPresent()) return -4;

    EVRInitError err = EVRInitError_VRInitError_None;
    pVR_InitInternal(&err, EVRApplicationType_VRApplication_Overlay);
    if (err != EVRInitError_VRInitError_None) return (int)err;

    // OpenVR's C API hands back the C-style function table ONLY when the version
    // is requested with the "FnTable:" prefix. Without it, GetGenericInterface
    // returns the C++ interface object (vtable ptr + data); reading that as a
    // VR_*_FnTable makes every method call jump through a garbage slot, which
    // faults with an execute access violation and crashes the process.
    char fnName[128];

    snprintf(fnName, sizeof(fnName), "FnTable:%s", IVROverlay_Version);
    intptr_t iface = pVR_GetGenericInterface(fnName, &err);
    if (!iface || err != EVRInitError_VRInitError_None) return err ? (int)err : -10;
    g_overlay = (struct VR_IVROverlay_FnTable *)iface;

    snprintf(fnName, sizeof(fnName), "FnTable:%s", IVRSystem_Version);
    iface = pVR_GetGenericInterface(fnName, &err);
    if (!iface || err != EVRInitError_VRInitError_None) return err ? (int)err : -11;
    g_system = (struct VR_IVRSystem_FnTable *)iface;

    return 0;
}

void wt_vr_shutdown(void) {
    if (pVR_ShutdownInternal) pVR_ShutdownInternal();
    g_overlay = NULL;
    g_system = NULL;
    if (g_dll) {
        FreeLibrary(g_dll);
        g_dll = NULL;
    }
    pVR_InitInternal = NULL;
    pVR_ShutdownInternal = NULL;
    pVR_GetGenericInterface = NULL;
}

int wt_vr_is_ready(void) {
    return g_overlay && g_system;
}

// Drain SteamVR's event queues for this overlay app. SteamVR delivers events
// (focus changes, dashboard interactions, timing, and crucially VREvent_Quit)
// to every overlay application and expects them to be polled continuously; an
// app that never drains them is eventually treated as unresponsive and its
// overlay stops updating — it freezes on the last submitted frame. Returns 1 if
// a Quit was seen so the caller can tear down and reconnect when SteamVR is back.
int wt_vr_pump_events(uint64_t handle) {
    int quit = 0;
    struct VREvent_t ev;
    if (g_overlay && handle) {
        while (g_overlay->PollNextOverlayEvent((VROverlayHandle_t)handle, &ev, sizeof(ev))) {
            if (ev.eventType == EVREventType_VREvent_Quit) quit = 1;
        }
    }
    if (g_system) {
        while (g_system->PollNextEvent(&ev, sizeof(ev))) {
            if (ev.eventType == EVREventType_VREvent_Quit) quit = 1;
        }
    }
    return quit;
}

uint64_t wt_vr_overlay_create(const char *key, const char *name) {
    if (!g_overlay) return 0;
    VROverlayHandle_t h = 0;
    EVROverlayError e = g_overlay->CreateOverlay((char *)key, (char *)name, &h);
    if (e != EVROverlayError_VROverlayError_None) return 0;
    g_overlay->SetOverlayInputMethod(h, VROverlayInputMethod_None);
    return (uint64_t)h;
}

void wt_vr_overlay_destroy(uint64_t handle) {
    if (!g_overlay || !handle) return;
    g_overlay->HideOverlay((VROverlayHandle_t)handle);
    g_overlay->DestroyOverlay((VROverlayHandle_t)handle);
}

int wt_vr_overlay_set_raw(uint64_t handle, const void *bgra, uint32_t width, uint32_t height) {
    if (!g_overlay || !handle || !bgra || width == 0 || height == 0) return 0;
    EVROverlayError e = g_overlay->SetOverlayRaw(
        (VROverlayHandle_t)handle, (void *)bgra, width, height, 4);
    return e == EVROverlayError_VROverlayError_None;
}

int wt_vr_overlay_show(uint64_t handle) {
    if (!g_overlay || !handle) return 0;
    return g_overlay->ShowOverlay((VROverlayHandle_t)handle) == EVROverlayError_VROverlayError_None;
}

int wt_vr_overlay_hide(uint64_t handle) {
    if (!g_overlay || !handle) return 0;
    return g_overlay->HideOverlay((VROverlayHandle_t)handle) == EVROverlayError_VROverlayError_None;
}

int wt_vr_overlay_set_width_meters(uint64_t handle, float width_m) {
    if (!g_overlay || !handle) return 0;
    return g_overlay->SetOverlayWidthInMeters((VROverlayHandle_t)handle, width_m) == EVROverlayError_VROverlayError_None;
}

int wt_vr_overlay_set_transform_hmd_offset(uint64_t handle, float distance_m, float width_m, float y_offset_m) {
    if (!g_overlay || !handle || distance_m < 0.2f || width_m < 0.05f) return 0;

    struct HmdMatrix34_t mat;
    memset(&mat, 0, sizeof(mat));
    mat.m[0][0] = 1.f;
    mat.m[1][1] = 1.f;
    mat.m[2][2] = 1.f;
    mat.m[0][3] = 0.f;
    mat.m[1][3] = y_offset_m;
    mat.m[2][3] = -distance_m;

    if (!wt_vr_overlay_set_width_meters(handle, width_m)) return 0;

    EVROverlayError e = g_overlay->SetOverlayTransformTrackedDeviceRelative(
        (VROverlayHandle_t)handle, (TrackedDeviceIndex_t)k_unTrackedDeviceIndex_Hmd, &mat);
    return e == EVROverlayError_VROverlayError_None;
}

int wt_vr_overlay_set_transform_hmd(uint64_t handle, float distance_m, float width_m, float aspect) {
    (void)aspect;
    return wt_vr_overlay_set_transform_hmd_offset(handle, distance_m, width_m, 0.f);
}

int wt_vr_overlay_set_transform_absolute(uint64_t handle, const float *matrix3x4) {
    if (!g_overlay || !handle || !matrix3x4) return 0;
    struct HmdMatrix34_t mat;
    memcpy(&mat, matrix3x4, sizeof(mat));
    EVROverlayError e = g_overlay->SetOverlayTransformAbsolute(
        (VROverlayHandle_t)handle,
        ETrackingUniverseOrigin_TrackingUniverseStanding,
        &mat);
    return e == EVROverlayError_VROverlayError_None;
}

int wt_vr_get_device_pose(uint32_t device_index, float *matrix3x4_out) {
    if (!g_system || !matrix3x4_out) return 0;
    struct TrackedDevicePose_t poses[WT_MAX_TRACKED_DEVICES];
    g_system->GetDeviceToAbsoluteTrackingPose(
        ETrackingUniverseOrigin_TrackingUniverseStanding,
        0.f, poses, WT_MAX_TRACKED_DEVICES);
    if (device_index >= WT_MAX_TRACKED_DEVICES || !poses[device_index].bPoseIsValid) return 0;
    memcpy(matrix3x4_out, &poses[device_index].mDeviceToAbsoluteTracking, sizeof(struct HmdMatrix34_t));
    return 1;
}

int wt_vr_get_hmd_position(float *x, float *y, float *z) {
    float m[12];
    if (!wt_vr_get_device_pose(k_unTrackedDeviceIndex_Hmd, m)) return 0;
    *x = m[3];
    *y = m[7];
    *z = m[11];
    return 1;
}
