package callctl

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

var (
	browserAPIOnce sync.Once
	browserAPI     *webrtc.API
	browserAPIErr  error
)

// browserWebRTCAPI returns a process-wide *webrtc.API for the browser-facing
// PeerConnections. When WAHA_WEBRTC_UDP_PORT is set, all ICE traffic is funneled
// through a single fixed UDP port and host candidates are advertised with
// WAHA_PUBLIC_IP (1:1 NAT, e.g. Docker bridge). Without the env vars it falls
// back to pion's default behavior (ephemeral ports, interface IPs) which works
// for localhost/LAN runs.
func browserWebRTCAPI() (*webrtc.API, error) {
	browserAPIOnce.Do(func() {
		port, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("WAHA_WEBRTC_UDP_PORT")))
		browserAPI, browserAPIErr = buildBrowserAPI(port, publicIPs())
	})
	return browserAPI, browserAPIErr
}

func buildBrowserAPI(udpPort int, externalIPs []string) (*webrtc.API, error) {
	if udpPort <= 0 {
		return webrtc.NewAPI(), nil
	}

	mux, err := ice.NewMultiUDPMuxFromPort(udpPort, ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4))
	if err != nil {
		return nil, err
	}

	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(mux)
	if len(externalIPs) > 0 {
		se.SetNAT1To1IPs(externalIPs, webrtc.ICECandidateTypeHost)
	}
	return webrtc.NewAPI(webrtc.WithSettingEngine(se)), nil
}

func publicIPs() []string {
	raw := strings.TrimSpace(os.Getenv("WAHA_PUBLIC_IP"))
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
