package projectassistanthost

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"

	servers "github.com/sainteye/clawdline/internal/app/botdevserver"
	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

func TestServerAddressesComeFromLiteralSourceBinds(t *testing.T) {
	interfaces := []net.Addr{}
	for _, text := range []string{"127.0.0.1/8", "192.0.2.23/24", "192.0.2.23/24", "::1/128", "fe80::123/64", "2001:db8::123/64"} {
		_, address, err := net.ParseCIDR(text)
		if err != nil {
			t.Fatal(err)
		}
		ip, _, _ := net.ParseCIDR(text)
		address.IP = ip
		interfaces = append(interfaces, address)
	}
	rows := []servers.Listener{{Address: "::", Port: 43123}, {Address: "127.0.0.1", Port: 43122}, {Address: "0.0.0.0", Port: 43123}, {Address: "192.0.2.23", Port: 43124}, {Address: "127.0.0.1", Port: 43122}}
	got, err := listenerPorts(rows, interfaces)
	want := []app.DevServerPort{
		{Port: 43122, Bind: "127.0.0.1", Local: "http://127.0.0.1:43122", Remote: []string{}},
		{Port: 43123, Bind: "0.0.0.0", Local: "http://127.0.0.1:43123", Remote: []string{"http://192.0.2.23:43123"}},
		{Port: 43123, Bind: "::", Local: "http://[::1]:43123", Remote: []string{"http://[2001:db8::123]:43123"}},
		{Port: 43124, Bind: "192.0.2.23", Local: "http://192.0.2.23:43124", Remote: []string{"http://192.0.2.23:43124"}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal(got, err)
	}
	for _, row := range []servers.Listener{{Address: "https://supplied.invalid/path", Port: 43123}, {Address: "localhost", Port: 43123}, {Address: "127.0.0.1", Port: 0}} {
		if got, err := listenerPorts([]servers.Listener{row}, interfaces); err == nil || got != nil {
			t.Fatal("nonliteral bind became a URL", got, err)
		}
	}
}

func TestServerResultsRetainIdentitiesWhenObservationsExceedTheFrameBudget(t *testing.T) {
	result := app.DevServerResult{Servers: []app.DevServerInfo{{ID: "one", Attempt: "first", PortsKnown: true, Ports: []app.DevServerPort{{Port: 43123, Remote: []string{strings.Repeat("x", capacity.MaximumBotFrameBytes)}}}}, {ID: "two", Attempt: "second", PortsKnown: true, Ports: []app.DevServerPort{}}}}
	got, err := boundedServerResult(result)
	if err != nil || len(got.Servers) != 2 || got.Servers[0].ID != "one" || got.Servers[1].Attempt != "second" {
		t.Fatal(got, err)
	}
	for _, info := range got.Servers {
		if info.PortsKnown || info.Ports == nil || len(info.Ports) != 0 {
			t.Fatal("partial observation advertised as complete", info)
		}
	}
}

func TestServerNativeAndHumanReadsShareCurrentSourceObservations(t *testing.T) {
	f, _, started := newServerHostFixture(t, func(context.Context) ([]servers.Listener, error) {
		return []servers.Listener{{Address: "127.0.0.1", Port: 43123}}, nil
	})
	bot := f.create(t, "observed-server")
	tools, session := beginServerNative(t, f, bot, "one")
	first := startReviewedServer(t, f, bot, tools, "start")
	_ = fixtureReceive(t, started)
	fixtureEventually(t, func() bool {
		out, err := tools.DevServers(context.Background(), app.DevServerRequest{Action: "list"})
		return err == nil && len(out.Servers) == 1 && out.Servers[0].PortsKnown && len(out.Servers[0].Ports) == 1
	})
	service, _ := f.h.Service()
	out, err := service.DevServers(context.Background(), bot.Key, "")
	if err != nil || len(out.Servers) != 1 || !out.Servers[0].PortsKnown || len(out.Servers[0].Ports) != 1 || len(out.Servers[0].Ports[0].Remote) != 0 {
		t.Fatal(out, err)
	}
	if out.Servers[0].Ports[0].Local != "http://127.0.0.1:43123" {
		t.Fatal(out)
	}
	if _, err := tools.DevServers(context.Background(), app.DevServerRequest{Action: "stop", Key: "stop", ID: first.ID}); err != nil {
		t.Fatal(err)
	}
	out, err = service.DevServers(context.Background(), bot.Key, "")
	if err != nil || len(out.Servers) != 1 || !out.Servers[0].PortsKnown || len(out.Servers[0].Ports) != 0 {
		t.Fatal("stopped observation", out, err)
	}
	session.reply <- app.PromptResult{FinalText: "Synthetic observation completed"}
}

func TestServerUnreadableObservationDoesNotInventAnEmptyPortSet(t *testing.T) {
	f, _, started := newServerHostFixture(t, func(context.Context) ([]servers.Listener, error) { return nil, errors.New("synthetic OS denial") })
	bot := f.create(t, "unreadable-server")
	tools, session := beginServerNative(t, f, bot, "one")
	_ = startReviewedServer(t, f, bot, tools, "start")
	_ = fixtureReceive(t, started)
	service, _ := f.h.Service()
	out, err := service.DevServers(context.Background(), bot.Key, "")
	if err != nil || len(out.Servers) != 1 || out.Servers[0].PortsKnown || out.Servers[0].Ports == nil || len(out.Servers[0].Ports) != 0 {
		t.Fatal(out, err)
	}
	session.reply <- app.PromptResult{FinalText: "Synthetic observation refused"}
}
