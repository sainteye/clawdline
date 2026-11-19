package projectassistanthost

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Listener details are optional observations. If they do not fit the existing
// native-frame budget, retain every server identity and mark all observations
// unknown rather than publish a partial set as complete.
func boundedServerResult(result app.DevServerResult) (app.DevServerResult, error) {
	body, err := json.Marshal(result)
	if err != nil || len(body) <= capacity.MaximumBotFrameBytes {
		return result, err
	}
	clear := func(info *app.DevServerInfo) { info.PortsKnown = false; info.Ports = []app.DevServerPort{} }
	for i := range result.Servers {
		clear(&result.Servers[i])
	}
	if result.Server != nil {
		clear(result.Server)
	}
	body, err = json.Marshal(result)
	if err == nil && len(body) > capacity.MaximumBotFrameBytes {
		err = failure(pa.CapacityFull, "server_result")
	}
	return result, err
}

func listenerURL(address netip.Addr, port uint16) string {
	return (&url.URL{Scheme: "http", Host: net.JoinHostPort(address.String(), strconv.Itoa(int(port)))}).String()
}

// Addresses are derived by the source from literal kernel binds and current
// interface addresses. Neither a client nor a log supplies one. A candidate
// URL does not promise HTTP service or reachability from another device.
func listenerPorts(rows []servers.Listener, interfaces []net.Addr) ([]app.DevServerPort, error) {
	result := []app.DevServerPort{}
	seen := map[netip.AddrPort]bool{}
	for _, row := range rows {
		bind, err := netip.ParseAddr(row.Address)
		if err != nil || row.Port == 0 {
			return nil, failure(pa.Invalid, "server_listener")
		}
		key := netip.AddrPortFrom(bind, row.Port)
		if seen[key] {
			continue
		}
		seen[key] = true
		local := bind
		if bind.IsUnspecified() {
			if bind.Is4() {
				local = netip.MustParseAddr("127.0.0.1")
			} else {
				local = netip.IPv6Loopback()
			}
		}
		port := app.DevServerPort{Port: int(row.Port), Bind: bind.String(), Local: listenerURL(local, row.Port), Remote: []string{}}
		remote := map[string]bool{}
		if bind.IsUnspecified() {
			for _, iface := range interfaces {
				prefix, err := netip.ParsePrefix(iface.String())
				if err != nil {
					continue
				}
				address := prefix.Addr()
				if address.Is4() != bind.Is4() || address.IsLoopback() || !address.IsGlobalUnicast() {
					continue
				}
				remote[listenerURL(address, row.Port)] = true
			}
		} else if !bind.IsLoopback() && bind.IsGlobalUnicast() {
			remote[listenerURL(bind, row.Port)] = true
		}
		for address := range remote {
			port.Remote = append(port.Remote, address)
		}
		sort.Strings(port.Remote)
		result = append(result, port)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port != result[j].Port {
			return result[i].Port < result[j].Port
		}
		return result[i].Bind < result[j].Bind
	})
	return result, nil
}

func (h *Host) observedServerInfo(ctx context.Context, r *servers.Registry, saved servers.SavedServer) app.DevServerInfo {
	info := serverInfo(saved)
	if r == nil {
		return info
	}
	rows, err := r.ObserveListeners(ctx, saved)
	if err != nil {
		return info
	}
	if len(rows) == 0 {
		info.PortsKnown = true
		return info
	}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return info
	}
	ports, err := listenerPorts(rows, interfaces)
	if err != nil {
		return info
	}
	info.PortsKnown, info.Ports = true, ports
	return info
}
