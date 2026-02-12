//go:build !windows

package window

// ConstrainToScreen is a no-op on non-Windows platforms.
func ConstrainToScreen(windowTitle string) error {
	return nil
}

// CenterAndResize is a no-op on non-Windows platforms.
func CenterAndResize(windowTitle string) error {
	return nil
}

// HideWindow is a no-op on non-Windows platforms.
func HideWindow(windowTitle string) error {
	return nil
}
