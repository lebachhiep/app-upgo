//go:build !(cgo || windows)

package tray

type TrayCallbacks struct {
	OnShowWindow   func()
	OnStartRelay   func()
	OnStopRelay    func()
	OnQuit         func()
	IsRelayRunning func() bool
}

type TrayController struct{}

func NewTrayController(cb TrayCallbacks) *TrayController { return &TrayController{} }
func (tc *TrayController) Start()                        {}
func (tc *TrayController) Stop()                         {}
func (tc *TrayController) SetRelayRunning(running bool)  {}
