//go:build !linux

package audiotags

// copyXattrs is a no-op off Linux, where Streamline's deployments run.
func copyXattrs(string, string) error { return nil }
