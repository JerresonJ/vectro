//go:build !cgo

package robot

// Vectro builds vic-cloud without CGO, so the Breakpad crash reporter isn't
// linked in. Go panics still reach the journal through logwrapper.

// InstallCrashReporter is a no-op without CGO.
func InstallCrashReporter(procname string) {}

// UninstallCrashReporter is a no-op without CGO.
func UninstallCrashReporter() {}
