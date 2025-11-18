package camera

// Logger interface for camera package to avoid circular dependencies
type Logger interface {
	Printf(format string, v ...interface{})
	Debugf(format string, v ...interface{})
	Fatalf(format string, v ...interface{})
}
