//go:build !darwin && !linux && !windows

package autostart

func New() (Manager, error) {
	return nil, ErrUnsupported
}
