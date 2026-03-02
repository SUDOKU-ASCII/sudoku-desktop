//go:build !darwin

package core

import "time"

func darwinListTunInterfaces() map[string]struct{} {
	return map[string]struct{}{}
}

func darwinWaitNewTunInterface(_ map[string]struct{}, _ time.Duration) string {
	return ""
}

func darwinFindTunInterfaceByIPv4(_ string) string {
	return ""
}
