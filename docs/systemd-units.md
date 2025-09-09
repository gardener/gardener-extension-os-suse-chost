# Handling of systemd units

During node bootstrap, the provision script performs some changes to existing systemd units which are listed here.

## Docker

Some versions of SuSE CHost come with a predefined docker unit - enabled but not started. In case of a reboot, the docker unit is started and prevents the containerd unit from starting.
Due to this reason, in the provision script, we update the containerd unit to do not conflict with the docker unit.

In addition, we are disabling the docker unit to prevent a reboot from starting it.
