#pragma once

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

int wt_vr_init(void);
void wt_vr_shutdown(void);
int wt_vr_is_ready(void);
int wt_vr_pump_events(uint64_t handle);
uint64_t wt_vr_overlay_create(const char *key, const char *name);
void wt_vr_overlay_destroy(uint64_t handle);
int wt_vr_overlay_set_raw(uint64_t handle, const void *bgra, uint32_t width, uint32_t height);
int wt_vr_overlay_show(uint64_t handle);
int wt_vr_overlay_hide(uint64_t handle);
int wt_vr_overlay_set_width_meters(uint64_t handle, float width_m);
int wt_vr_overlay_set_transform_hmd(uint64_t handle, float distance_m, float width_m, float aspect);
int wt_vr_overlay_set_transform_hmd_offset(uint64_t handle, float distance_m, float width_m, float y_offset_m);
int wt_vr_overlay_set_transform_absolute(uint64_t handle, const float *matrix3x4);
int wt_vr_get_device_pose(uint32_t device_index, float *matrix3x4_out);
int wt_vr_get_hmd_position(float *x, float *y, float *z);

#ifdef __cplusplus
}
#endif
