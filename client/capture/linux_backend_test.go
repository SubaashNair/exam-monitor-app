package capture

import (
	"errors"
	"os/exec"
	"testing"
)

// makeLookPath returns a fake LookPath that resolves any name in `present` to
// /usr/bin/<name>, and returns exec.ErrNotFound otherwise.
func makeLookPath(present ...string) LookPath {
	set := make(map[string]bool, len(present))
	for _, p := range present {
		set[p] = true
	}
	return func(name string) (string, error) {
		if set[name] {
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
}

func TestPickLinuxBackend(t *testing.T) {
	tests := []struct {
		name        string
		sessionType string
		present     []string
		wantBackend Backend
		wantPath    string
		wantErr     bool
	}{
		{
			name:        "x11 session",
			sessionType: "x11",
			present:     []string{},
			wantBackend: BackendX11,
			wantPath:    "",
		},
		{
			name:        "empty session type defaults to x11",
			sessionType: "",
			present:     []string{},
			wantBackend: BackendX11,
			wantPath:    "",
		},
		{
			name:        "tty session type maps to x11",
			sessionType: "tty",
			present:     []string{},
			wantBackend: BackendX11,
			wantPath:    "",
		},
		{
			name:        "wayland with grim only",
			sessionType: "wayland",
			present:     []string{"grim"},
			wantBackend: BackendWaylandGrim,
			wantPath:    "/usr/bin/grim",
		},
		{
			name:        "wayland with gnome-screenshot only",
			sessionType: "wayland",
			present:     []string{"gnome-screenshot"},
			wantBackend: BackendWaylandGnomeScreenshot,
			wantPath:    "/usr/bin/gnome-screenshot",
		},
		{
			name:        "wayland with spectacle only",
			sessionType: "wayland",
			present:     []string{"spectacle"},
			wantBackend: BackendWaylandSpectacle,
			wantPath:    "/usr/bin/spectacle",
		},
		{
			name:        "wayland with grim and others — grim wins",
			sessionType: "wayland",
			present:     []string{"grim", "gnome-screenshot", "spectacle"},
			wantBackend: BackendWaylandGrim,
			wantPath:    "/usr/bin/grim",
		},
		{
			name:        "wayland with gnome and spectacle — gnome wins (priority)",
			sessionType: "wayland",
			present:     []string{"gnome-screenshot", "spectacle"},
			wantBackend: BackendWaylandGnomeScreenshot,
			wantPath:    "/usr/bin/gnome-screenshot",
		},
		{
			name:        "wayland with no tools — error",
			sessionType: "wayland",
			present:     []string{},
			wantBackend: BackendUnknown,
			wantPath:    "",
			wantErr:     true,
		},
		{
			name:        "unknown session type — error",
			sessionType: "mir",
			present:     []string{},
			wantBackend: BackendUnknown,
			wantPath:    "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotBackend, gotPath, err := PickLinuxBackend(tt.sessionType, makeLookPath(tt.present...))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if gotBackend != tt.wantBackend {
				t.Errorf("backend = %v, want %v", gotBackend, tt.wantBackend)
			}
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

func TestPickLinuxBackend_NoToolErrorIsTyped(t *testing.T) {
	_, _, err := PickLinuxBackend("wayland", makeLookPath())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrNoWaylandTool) {
		t.Errorf("expected error to wrap ErrNoWaylandTool; got: %v", err)
	}
}

func TestBackendString(t *testing.T) {
	tests := []struct {
		b    Backend
		want string
	}{
		{BackendUnknown, "unknown"},
		{BackendX11, "x11"},
		{BackendWaylandGrim, "wayland-grim"},
		{BackendWaylandGnomeScreenshot, "wayland-gnome-screenshot"},
		{BackendWaylandSpectacle, "wayland-spectacle"},
	}

	for _, tt := range tests {
		if got := tt.b.String(); got != tt.want {
			t.Errorf("Backend(%d).String() = %q, want %q", tt.b, got, tt.want)
		}
	}
}
