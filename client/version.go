package main

// Version is the running client's release version. CI overrides this via
// -ldflags "-X main.Version=v0.1.x" at build time (see release.yml). Local
// builds get the "dev" default, which causes the updater notifier to
// silently skip the version comparison.
var Version = "dev"
