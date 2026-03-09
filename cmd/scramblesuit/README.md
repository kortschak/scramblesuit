## `scramblesuit`

Video obfuscation via v4l2loopback using dipole Voronoi subdivision.

### Requirements

```
sudo apt install v4l-utils v4l2loopback-utils
```

You will also need to install the v4l2loopback kernel module. This may require building from [source](https://github.com/v4l2loopback/v4l2loopback) depending on your distribution.

### Create the virtual device

```
sudo modprobe v4l2loopback exclusive_caps=1 video_nr=10 card_label="Scramble-Suit"
```

### Running the Filter

You can run the filter and immediately see the results in any application (like VLC or Zoom) by selecting the "Scramble-Suit" source.

```
scramblesuit -loopback /dev/video10 [...] &
```

```
vlc v4l2:///dev/video10
```
