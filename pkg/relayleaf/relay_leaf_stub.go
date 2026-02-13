package relayleaf

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"time"
)

type Stats struct {
	UptimeSeconds     int64
	TotalStreams       int64
	BytesSent         int64
	BytesReceived     int64
	ReconnectCount    int64
	LastError         string
	ExitPointsJSON    string
	NodeAddressesJSON string
	ActiveStreams      int32
	ConnectedNodes    int32
	Connected         bool
}

type Client struct {
	mu          sync.RWMutex
	verbose     bool
	running     bool
	partnerId   string
	discoveryUrl string
	proxies     []string
	startTime   time.Time
	bytesSent   int64
	bytesRecv   int64
	streams     int64
	deviceId    string
}

func NewClient(verbose bool) (*Client, error) {
	return &Client{
		verbose:  verbose,
		proxies:  make([]string, 0),
		deviceId: generateDeviceID(""),
	}, nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running = false
	return nil
}

func (c *Client) SetDiscoveryURL(url string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.discoveryUrl = url
	return nil
}

func (c *Client) SetPartnerID(partnerID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partnerId = partnerID
	c.deviceId = generateDeviceID(partnerID)
	return nil
}

func (c *Client) AddProxy(proxyURL string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proxies = append(c.proxies, proxyURL)
	return nil
}

func (c *Client) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return fmt.Errorf("already started")
	}

	c.running = true
	c.startTime = time.Now()
	c.bytesSent = 0
	c.bytesRecv = 0
	c.streams = 0
	return nil
}

func (c *Client) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return fmt.Errorf("not started")
	}

	c.running = false
	return nil
}

func (c *Client) GetDeviceID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deviceId
}

func (c *Client) GetStats() (*Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return &Stats{}, nil
	}

	c.bytesSent += int64(rand.Intn(50000) + 10000)
	c.bytesRecv += int64(rand.Intn(80000) + 20000)
	c.streams += int64(rand.Intn(3))

	uptime := int64(time.Since(c.startTime).Seconds())

	return &Stats{
		UptimeSeconds:     uptime,
		TotalStreams:       c.streams,
		BytesSent:         c.bytesSent,
		BytesReceived:     c.bytesRecv,
		ReconnectCount:    0,
		LastError:         "",
		ExitPointsJSON:    "[]",
		NodeAddressesJSON: "[]",
		ActiveStreams:      int32(rand.Intn(5) + 1),
		ConnectedNodes:    int32(rand.Intn(3) + 1),
		Connected:         true,
	}, nil
}

func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

func Version() string {
	return "1.0.0-stub"
}

func generateDeviceID(partnerID string) string {
	hostname, _ := os.Hostname()
	seed := hostname + "-" + partnerID
	hash := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("rl-%x", hash[:8])
}
