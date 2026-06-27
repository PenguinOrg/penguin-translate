#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "openvr_capi.h"

static struct {
    uint32_t set_raw_w, set_raw_h, bpp;
    uint64_t byte_sum;
    int shown, show_count, transform_set, create_count;
} g_rec;

static void write_record(void) {
    const char *out = getenv("WT_FAKE_VR_OUT");
    if (!out || !out[0]) return;
    FILE *f = fopen(out, "w");
    if (!f) return;
    fprintf(f,
        "{\"set_raw_w\":%u,\"set_raw_h\":%u,\"bpp\":%u,\"byte_sum\":%llu,"
        "\"shown\":%d,\"show_count\":%d,\"transform_set\":%d,\"create_count\":%d}\n",
        g_rec.set_raw_w, g_rec.set_raw_h, g_rec.bpp,
        (unsigned long long)g_rec.byte_sum, g_rec.shown, g_rec.show_count,
        g_rec.transform_set, g_rec.create_count);
    fclose(f);
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_CreateOverlay(
    char *key, char *name, VROverlayHandle_t *handle) {
    (void)key; (void)name;
    g_rec.create_count++;
    if (handle) *handle = 0x4242;
    write_record();
    return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_DestroyOverlay(VROverlayHandle_t h) {
    (void)h; return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_SetOverlayInputMethod(
    VROverlayHandle_t h, VROverlayInputMethod m) {
    (void)h; (void)m; return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_SetOverlayRaw(
    VROverlayHandle_t h, void *buf, uint32_t w, uint32_t height, uint32_t bpp) {
    (void)h;
    g_rec.set_raw_w = w;
    g_rec.set_raw_h = height;
    g_rec.bpp = bpp;
    g_rec.byte_sum = 0;
    if (buf) {
        const unsigned char *p = (const unsigned char *)buf;
        size_t n = (size_t)w * height * bpp;
        for (size_t i = 0; i < n; i++) g_rec.byte_sum += p[i];
    }
    write_record();
    return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_ShowOverlay(VROverlayHandle_t h) {
    (void)h; g_rec.shown = 1; g_rec.show_count++; write_record();
    return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_HideOverlay(VROverlayHandle_t h) {
    (void)h; g_rec.shown = 0; write_record();
    return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_SetOverlayWidthInMeters(
    VROverlayHandle_t h, float m) {
    (void)h; (void)m; return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_SetOverlayTransformTrackedDeviceRelative(
    VROverlayHandle_t h, TrackedDeviceIndex_t dev, struct HmdMatrix34_t *mat) {
    (void)h; (void)dev; (void)mat; g_rec.transform_set = 1; write_record();
    return EVROverlayError_VROverlayError_None;
}

static EVROverlayError OPENVR_FNTABLE_CALLTYPE fake_SetOverlayTransformAbsolute(
    VROverlayHandle_t h, ETrackingUniverseOrigin o, struct HmdMatrix34_t *mat) {
    (void)h; (void)o; (void)mat; g_rec.transform_set = 1; write_record();
    return EVROverlayError_VROverlayError_None;
}

static bool OPENVR_FNTABLE_CALLTYPE fake_PollNextOverlayEvent(
    VROverlayHandle_t h, struct VREvent_t *ev, uint32_t cb) {
    (void)h; (void)ev; (void)cb; return false;
}

static bool OPENVR_FNTABLE_CALLTYPE fake_PollNextEvent(struct VREvent_t *ev, uint32_t cb) {
    (void)ev; (void)cb; return false;
}

static void OPENVR_FNTABLE_CALLTYPE fake_GetDeviceToAbsoluteTrackingPose(
    ETrackingUniverseOrigin o, float secs, struct TrackedDevicePose_t *poses, uint32_t count) {
    (void)o; (void)secs;
    if (poses && count) memset(poses, 0, sizeof(struct TrackedDevicePose_t) * count);
}

static struct VR_IVROverlay_FnTable g_overlay = {
    .CreateOverlay = fake_CreateOverlay,
    .DestroyOverlay = fake_DestroyOverlay,
    .SetOverlayInputMethod = fake_SetOverlayInputMethod,
    .SetOverlayRaw = fake_SetOverlayRaw,
    .ShowOverlay = fake_ShowOverlay,
    .HideOverlay = fake_HideOverlay,
    .SetOverlayWidthInMeters = fake_SetOverlayWidthInMeters,
    .SetOverlayTransformTrackedDeviceRelative = fake_SetOverlayTransformTrackedDeviceRelative,
    .SetOverlayTransformAbsolute = fake_SetOverlayTransformAbsolute,
    .PollNextOverlayEvent = fake_PollNextOverlayEvent,
};

static struct VR_IVRSystem_FnTable g_system = {
    .PollNextEvent = fake_PollNextEvent,
    .GetDeviceToAbsoluteTrackingPose = fake_GetDeviceToAbsoluteTrackingPose,
};

intptr_t VR_InitInternal(EVRInitError *err, EVRApplicationType type) {
    (void)type;
    if (err) *err = EVRInitError_VRInitError_None;
    return 1;
}

void VR_ShutdownInternal(void) { write_record(); }

bool VR_IsHmdPresent(void) { return true; }

bool VR_IsRuntimeInstalled(void) { return true; }

intptr_t VR_GetGenericInterface(const char *name, EVRInitError *err) {
    if (err) *err = EVRInitError_VRInitError_None;
    if (name && strstr(name, "IVROverlay")) return (intptr_t)&g_overlay;
    if (name && strstr(name, "IVRSystem")) return (intptr_t)&g_system;
    if (err) *err = (EVRInitError)1;
    return 0;
}
