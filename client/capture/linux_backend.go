package capture

import (
	"errors"
	"fmt"
)

// Backend identifies which Linux capture strategy to use.
type Backend int

const (
	BackendUnknown Backend = iota
	BackendX11
	BackendWaylandGrim            // wlroots compositors (Sway, Hyprland, river)
	BackendWaylandGnomeScreenshot // GNOME on Wayland
	BackendWaylandSpectacle       // KDE on Wayland
)

func (b Backend) String() string {
	switch b {
	case BackendX11:
		return "x11"
	case BackendWaylandGrim:
		return "wayland-grim"
	case BackendWaylandGnomeScreenshot:
		return "wayland-gnome-screenshot"
	case BackendWaylandSpectacle:
		return "wayland-spectacle"
	default:
		return "unknown"
	}
}

// LookPath is the dependency-injection shape for exec.LookPath. Tests inject
// fakes; production uses the stdlib.
type LookPath func(name string) (string, error)

// PickLinuxBackend chooses the capture backend based on the session type and
// availability of Wayland screenshot tools. Wayland fallback order is:
//
//	grim (wlroots: Sway, Hyprland, river)
//	gnome-screenshot (GNOME)
//	spectacle (KDE)
//
// Returns the chosen Backend, the absolute path of the chosen tool (empty for
// X11), and a typed error if Wayland but no tool was found.
func PickLinuxBackend(sessionType string, look LookPath) (Backend, string, error) {
	if sessionType == "wayland" {
		// Try tools in priority order.
		for _, tool := range []struct {
			name    string
			backend Backend
		}{
			{"grim", BackendWaylandGrim},
			{"gnome-screenshot", BackendWaylandGnomeScreenshot},
			{"spectacle", BackendWaylandSpectacle},
		} {
			if path, err := look(tool.name); err == nil && path != "" {
				return tool.backend, path, nil
			}
		}
		return BackendUnknown, "", fmt.Errorf("%w: install one of grim, gnome-screenshot, or spectacle", ErrNoWaylandTool)
	}
	// Default and "x11" both use X11.
	if sessionType == "" || sessionType == "x11" || sessionType == "tty" {
		return BackendX11, "", nil
	}
	return BackendUnknown, "", errors.New("capture: unrecognised XDG_SESSION_TYPE: " + sessionType)
}
