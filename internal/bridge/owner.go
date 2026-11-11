package bridge

// Owner is the program that listens on Bridge's port.
type Owner struct {
	PID    uint32
	Path   string // the executable, if it could be read
	Signed bool   // a valid Authenticode signature (Windows only)
}
