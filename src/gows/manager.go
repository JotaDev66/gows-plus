package gows

import (
	"context"
	"errors"
	gowsLog "github.com/devlikeapro/gows/log"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	waLog "go.mau.fi/whatsmeow/util/log"
	"strings"
	"sync"
)

var ErrSessionNotFound = errors.New("session not found")

// SessionManager control sessions in thread-safe way
type SessionManager struct {
	sessions     map[string]*GoWS
	sessionsLock *sync.RWMutex
	log          waLog.Logger
}

type StoreConfig struct {
	Dialect string
	Address string
}

type StorageConfig struct {
	Messages bool
	Groups   bool
	Chats    bool
	Labels   bool
}

func DefaultStorageConfig() StorageConfig {
	return StorageConfig{
		Messages: true,
		Groups:   true,
		Chats:    true,
		Labels:   true,
	}
}

type LogConfig struct {
	Level string
}

type ProxyConfig struct {
	Url string
}

type IgnoreJidsConfig struct {
	// Status indicates whether to ignore the special status broadcast JID (status@broadcast)
	// Note: Only applies to the "status@broadcast" JID.
	Status bool
	// Groups indicate whether to ignore JIDs with server type GroupServer (g.us)
	Groups bool
	// Newsletters indicate whether to ignore JIDs with server type NewsletterServer (newsletter)
	// This corresponds to WhatsApp Channels.
	Newsletters bool
	// Broadcast indicates whether to ignore broadcast list JIDs (types.BroadcastServer),
	// excluding the special "status@broadcast" JID which is controlled by the Status flag above.
	Broadcast bool
}

// SessionConfig contains configuration for a WhatsApp session.
type SessionConfig struct {
	Store   StoreConfig
	Storage StorageConfig
	Log     LogConfig
	Proxy   ProxyConfig
	Ignore  *IgnoreJidsConfig
}

func SetDeviceAndBrowser(device string, browser string) {
	store.DeviceProps.PlatformType = browserPlatformType(browser)
	store.SetOSInfo(device, [3]uint32{22, 0, 4})
}

// statusParticipantsBatchSize is the number of contacts per batch when sending to status@broadcast.
// Set at startup via SetStatusParticipantsBatchSize; defaults to 500.
var statusParticipantsBatchSize = 500

func SetStatusParticipantsBatchSize(n int) {
	statusParticipantsBatchSize = n
}

func GetDeviceProps() *waCompanionReg.DeviceProps {
	return store.DeviceProps
}

func browserPlatformType(name string) *waCompanionReg.DeviceProps_PlatformType {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "chrome":
		return waCompanionReg.DeviceProps_CHROME.Enum()
	case "firefox":
		return waCompanionReg.DeviceProps_FIREFOX.Enum()
	case "ie":
		return waCompanionReg.DeviceProps_IE.Enum()
	case "opera":
		return waCompanionReg.DeviceProps_OPERA.Enum()
	case "safari":
		return waCompanionReg.DeviceProps_SAFARI.Enum()
	case "edge":
		return waCompanionReg.DeviceProps_EDGE.Enum()
	default:
		return waCompanionReg.DeviceProps_UNKNOWN.Enum()
	}
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions:     make(map[string]*GoWS),
		sessionsLock: &sync.RWMutex{},
		log:          gowsLog.Stdout("Manager", "DEBUG", false),
	}
}

func (sm *SessionManager) Build(name string, cfg SessionConfig) (*GoWS, error) {
	sm.sessionsLock.Lock()
	defer sm.sessionsLock.Unlock()
	gows, err := sm.unlockedBuild(name, cfg)
	if err != nil {
		sm.log.Errorf("Error building session '%s': %v", name, err)
		return nil, err
	}
	return gows, nil
}

func (sm *SessionManager) unlockedBuild(name string, cfg SessionConfig) (*GoWS, error) {
	if goWS, ok := sm.sessions[name]; ok {
		return goWS, nil
	}
	sm.log.Debugf("Building session '%s'...", name)

	ctx := context.WithValue(context.Background(), "name", name)
	log := gowsLog.Stdout("Session", cfg.Log.Level, false)

	dialect := cfg.Store.Dialect
	address := cfg.Store.Address
	gows, err := BuildSession(ctx, log.Sub(name), dialect, address, cfg.Ignore, cfg.Storage)
	if err != nil {
		return nil, err
	}
	sm.sessions[name] = gows

	err = gows.SetProxyAddress(cfg.Proxy.Url)
	if err != nil {
		delete(sm.sessions, name)
		return nil, err
	}
	sm.log.Infof("Session has been built '%s'", name)
	return gows, nil
}

func (sm *SessionManager) Start(name string) error {
	sm.log.Infof("Starting session '%s'...", name)
	sm.sessionsLock.RLock()
	goWS, ok := sm.sessions[name]
	sm.sessionsLock.RUnlock()
	if !ok {
		return ErrSessionNotFound
	}
	if err := goWS.Start(); err != nil {
		sm.log.Errorf("Error starting session '%s': %v", name, err)
		return err
	}
	sm.log.Infof("Session started '%s'", name)
	return nil
}

func (sm *SessionManager) Get(name string) (*GoWS, error) {
	sm.sessionsLock.RLock()
	defer sm.sessionsLock.RUnlock()

	if goWS, ok := sm.sessions[name]; !ok {
		return nil, ErrSessionNotFound
	} else {
		return goWS, nil
	}
}

func (sm *SessionManager) Stop(name string) {
	sm.log.Infof("Stopping session '%s'...", name)
	sm.sessionsLock.Lock()
	goWS, ok := sm.sessions[name]
	if ok {
		delete(sm.sessions, name)
	}
	sm.sessionsLock.Unlock()
	if ok {
		goWS.Stop()
	}
	sm.log.Infof("Session stopped '%s'", name)
}
