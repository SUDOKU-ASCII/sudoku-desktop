package core

import "testing"

func TestParseDarwinNetstatRoutes(t *testing.T) {
	out := `
Routing tables

Internet:
Destination        Gateway            Flags        Netif Expire
default            192.168.1.1        UGSc           en0
default            link#20            UCS            utun6
127                127.0.0.1          UCS             lo0
`
	routes := parseDarwinNetstatRoutes(out)
	if len(routes) != 3 {
		t.Fatalf("unexpected routes length: %d", len(routes))
	}
	if routes[0].Destination != "default" || routes[0].Gateway != "192.168.1.1" || routes[0].Netif != "en0" {
		t.Fatalf("unexpected route[0]: %#v", routes[0])
	}
	if routes[1].Destination != "default" || routes[1].Gateway != "link#20" || routes[1].Netif != "utun6" {
		t.Fatalf("unexpected route[1]: %#v", routes[1])
	}
	if routes[2].Destination != "127" || routes[2].Gateway != "127.0.0.1" || routes[2].Netif != "lo0" {
		t.Fatalf("unexpected route[2]: %#v", routes[2])
	}
}
