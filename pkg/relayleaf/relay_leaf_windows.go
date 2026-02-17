//go:build windows

package relayleaf

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

// cStats mirrors the C struct layout written by relay_leaf_get_stats.
type cStats struct {
	UptimeSeconds     int64
	TotalStreams      int64
	BytesSent         int64
	BytesReceived     int64
	ReconnectCount    int64
	LastError         uintptr // *C.char
	ExitPointsJSON    uintptr // *C.char
	NodeAddressesJSON uintptr // *C.char
	ActiveStreams     int32
	ConnectedNodes    int32
	Connected         int32
}

// Stats matches the public API consumed by relay.RelayManager.
type Stats struct {
	UptimeSeconds     int64
	TotalStreams      int64
	BytesSent         int64
	BytesReceived     int64
	ReconnectCount    int64
	LastError         string
	ExitPointsJSON    string
	NodeAddressesJSON string
	ActiveStreams     int32
	ConnectedNodes    int32
	Connected         bool
}

// dllProcs holds all resolved DLL procedures.
type dllProcs struct {
	dll             *syscall.DLL
	create          *syscall.Proc
	destroy         *syscall.Proc
	setPartnerID    *syscall.Proc
	setDiscoveryURL *syscall.Proc
	addProxy        *syscall.Proc
	start           *syscall.Proc
	stop            *syscall.Proc
	getDeviceID     *syscall.Proc
	getStats        *syscall.Proc
	freeString      *syscall.Proc
	version         *syscall.Proc
}

var (
	loadMu sync.Mutex
	procs  *dllProcs
)

// loadDLL attempts to load the relay leaf DLL from the same directory as the executable.
func loadDLL() *dllProcs {
	loadMu.Lock()
	defer loadMu.Unlock()

	if procs != nil {
		return procs
	}

	libName := GetLibraryName()
	if libName == "" {
		return nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return nil
	}
	dllPath := filepath.Join(filepath.Dir(exePath), libName)

	dll, err := syscall.LoadDLL(dllPath)
	if err != nil {
		return nil
	}

	p := &dllProcs{dll: dll}
	var ok bool
	if p.create, ok = findProc(dll, "relay_leaf_create"); !ok {
		return nil
	}
	if p.destroy, ok = findProc(dll, "relay_leaf_destroy"); !ok {
		return nil
	}
	if p.setPartnerID, ok = findProc(dll, "relay_leaf_set_partner_id"); !ok {
		return nil
	}
	if p.setDiscoveryURL, ok = findProc(dll, "relay_leaf_set_discovery_url"); !ok {
		return nil
	}
	if p.addProxy, ok = findProc(dll, "relay_leaf_add_proxy"); !ok {
		return nil
	}
	if p.start, ok = findProc(dll, "relay_leaf_start"); !ok {
		return nil
	}
	if p.stop, ok = findProc(dll, "relay_leaf_stop"); !ok {
		return nil
	}
	if p.getDeviceID, ok = findProc(dll, "relay_leaf_get_device_id"); !ok {
		return nil
	}
	if p.getStats, ok = findProc(dll, "relay_leaf_get_stats"); !ok {
		return nil
	}
	if p.freeString, ok = findProc(dll, "relay_leaf_free_string"); !ok {
		return nil
	}
	if p.version, ok = findProc(dll, "relay_leaf_version"); !ok {
		return nil
	}

	procs = p
	return procs
}

func findProc(dll *syscall.DLL, name string) (*syscall.Proc, bool) {
	proc, err := dll.FindProc(name)
	return proc, err == nil
}

// Client wraps either a real DLL handle or stub state.
type Client struct {
	mu       sync.RWMutex
	handle   uintptr   // cgo.Handle from DLL (>0 = real mode)
	procs    *dllProcs // non-nil = real mode
	stub     bool      // true = stub fallback
	stubData *stubState
}

type stubState struct {
	verbose      bool
	running      bool
	partnerId    string
	discoveryUrl string
	proxies      []string
	deviceId     string
}

func NewClient(verbose bool) (*Client, error) {
	p := loadDLL()
	if p != nil {
		v := uintptr(0)
		if verbose {
			v = 1
		}
		var handle uintptr
		ret, _, _ := p.create.Call(v, uintptr(unsafe.Pointer(&handle)))
		if ret != 0 || handle == 0 {
			return newStubClient(verbose), nil
		}
		return &Client{handle: handle, procs: p}, nil
	}
	return newStubClient(verbose), nil
}

func newStubClient(verbose bool) *Client {
	return &Client{
		stub: true,
		stubData: &stubState{
			verbose:  verbose,
			deviceId: generateStubDeviceID(""),
		},
	}
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.stub && c.handle != 0 {
		c.procs.destroy.Call(c.handle)
		c.handle = 0
	}
	if c.stubData != nil {
		c.stubData.running = false
	}
	return nil
}

func (c *Client) SetDiscoveryURL(url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stub {
		c.stubData.discoveryUrl = url
		return nil
	}
	cstr := cString(url)
	ret, _, _ := c.procs.setDiscoveryURL.Call(c.handle, uintptr(unsafe.Pointer(&cstr[0])))
	if ret != 0 {
		return fmt.Errorf("set_discovery_url failed: code %d", ret)
	}
	return nil
}

func (c *Client) SetPartnerID(partnerID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stub {
		c.stubData.partnerId = partnerID
		c.stubData.deviceId = generateStubDeviceID(partnerID)
		return nil
	}
	cstr := cString(partnerID)
	ret, _, _ := c.procs.setPartnerID.Call(c.handle, uintptr(unsafe.Pointer(&cstr[0])))
	if ret != 0 {
		return fmt.Errorf("set_partner_id failed: code %d", ret)
	}
	return nil
}

func (c *Client) AddProxy(proxyURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stub {
		c.stubData.proxies = append(c.stubData.proxies, proxyURL)
		return nil
	}
	cstr := cString(proxyURL)
	ret, _, _ := c.procs.addProxy.Call(c.handle, uintptr(unsafe.Pointer(&cstr[0])))
	if ret != 0 {
		return fmt.Errorf("add_proxy failed: code %d", ret)
	}
	return nil
}

func (c *Client) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stub {
		if c.stubData.running {
			return fmt.Errorf("already started")
		}
		c.stubData.running = true
		return nil
	}
	ret, _, _ := c.procs.start.Call(c.handle)
	if ret != 0 {
		return fmt.Errorf("start failed: code %d", ret)
	}
	return nil
}

func (c *Client) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stub {
		if !c.stubData.running {
			return fmt.Errorf("not started")
		}
		c.stubData.running = false
		return nil
	}
	ret, _, _ := c.procs.stop.Call(c.handle)
	if ret != 0 {
		return fmt.Errorf("stop failed: code %d", ret)
	}
	return nil
}

func (c *Client) GetDeviceID() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stub {
		return c.stubData.deviceId
	}
	if c.handle == 0 {
		return ""
	}
	ret, _, _ := c.procs.getDeviceID.Call(c.handle)
	if ret == 0 {
		return ""
	}
	s := goStringFromPtr(ret)
	c.procs.freeString.Call(ret)
	return s
}

func (c *Client) GetStats() (*Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.stub {
		return &Stats{Connected: c.stubData.running}, nil
	}

	if c.handle == 0 {
		return &Stats{}, nil
	}

	var cs cStats
	ret, _, _ := c.procs.getStats.Call(c.handle, uintptr(unsafe.Pointer(&cs)))
	if ret != 0 {
		return &Stats{}, nil
	}

	return &Stats{
		UptimeSeconds:     cs.UptimeSeconds,
		TotalStreams:      cs.TotalStreams,
		BytesSent:         cs.BytesSent,
		BytesReceived:     cs.BytesReceived,
		ReconnectCount:    cs.ReconnectCount,
		LastError:         goStringFromPtr(cs.LastError),
		ExitPointsJSON:    goStringFromPtr(cs.ExitPointsJSON),
		NodeAddressesJSON: goStringFromPtr(cs.NodeAddressesJSON),
		ActiveStreams:     cs.ActiveStreams,
		ConnectedNodes:    cs.ConnectedNodes,
		Connected:         cs.Connected != 0,
	}, nil
}

func (c *Client) IsConnected() bool {
	stats, err := c.GetStats()
	if err != nil {
		return false
	}
	return stats.Connected
}

func Version() string {
	p := loadDLL()
	if p != nil {
		ret, _, _ := p.version.Call()
		if ret != 0 {
			s := goStringFromPtr(ret)
			p.freeString.Call(ret)
			return s
		}
	}
	return "1.0.0-stub"
}

// ── helpers ──────────────────────────────────────────────

func cString(s string) []byte {
	return append([]byte(s), 0)
}

func goStringFromPtr(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	var buf []byte
	for {
		b := *(*byte)(unsafe.Pointer(ptr))
		if b == 0 {
			break
		}
		buf = append(buf, b)
		ptr++
	}
	return string(buf)
}

func generateStubDeviceID(partnerID string) string {
	hostname, _ := os.Hostname()
	seed := hostname + "-" + partnerID
	h := [32]byte{}
	for i, b := range []byte(seed) {
		h[i%32] ^= b
	}
	return fmt.Sprintf("rl-%x", h[:8])
}
