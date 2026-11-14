resource "unifi_protect_sensor" "closet_motion" {
  mac  = "24:A4:3C:3D:FE:B9"
  name = "Main Closet Motion Sensor"

  motion_enabled     = true
  motion_sensitivity = 60
}

resource "unifi_protect_sensor" "main_floor_smoke" {
  mac  = "D0:21:F9:8B:6A:ED"
  name = "Main Floor Smoke Detector"
}
