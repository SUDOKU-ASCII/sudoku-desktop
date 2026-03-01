package core

import (
	"sort"
	"strings"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
)

type trafficSampleState struct {
	lastTx        uint64
	lastRx        uint64
	lastAt        time.Time
	lastDirectDec uint64
	lastProxyDec  uint64
}

func lookupInterfaceCounters(name string) (uint64, uint64, bool) {
	counters, err := gnet.IOCounters(true)
	if err != nil {
		return 0, 0, false
	}
	name = strings.TrimSpace(strings.ToLower(name))
	for _, c := range counters {
		if strings.ToLower(c.Name) == name {
			return c.BytesSent, c.BytesRecv, true
		}
	}
	return 0, 0, false
}

func topConnections(m map[string]*ActiveConnection, max int) []ActiveConnection {
	if max <= 0 {
		max = 200
	}
	out := make([]ActiveConnection, 0, len(m))
	for _, v := range m {
		if v == nil {
			continue
		}
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}
