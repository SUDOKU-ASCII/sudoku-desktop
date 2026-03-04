package core

import (
	"bufio"
	"strings"
)

type darwinNetstatRoute struct {
	Destination string
	Gateway     string
	Flags       string
	Netif       string
}

func parseDarwinNetstatRoutes(out string) []darwinNetstatRoute {
	var routes []darwinNetstatRoute
	s := bufio.NewScanner(strings.NewReader(out))
	inTable := false
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if !inTable {
			// netstat -rn header
			if len(fields) >= 2 && fields[0] == "Destination" && fields[1] == "Gateway" {
				inTable = true
			}
			continue
		}
		// Destination Gateway Flags Netif [Expire]
		if len(fields) < 4 {
			continue
		}
		if len(fields) >= 2 && fields[0] == "Destination" && fields[1] == "Gateway" {
			continue
		}
		routes = append(routes, darwinNetstatRoute{
			Destination: fields[0],
			Gateway:     fields[1],
			Flags:       fields[2],
			Netif:       fields[3],
		})
	}
	return routes
}
