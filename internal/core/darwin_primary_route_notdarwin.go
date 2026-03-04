//go:build !darwin

package core

import "errors"

func darwinPrimaryNetworkInfo() (darwinPrimaryRouteInfo, error) {
	return darwinPrimaryRouteInfo{}, errors.New("darwinPrimaryNetworkInfo is only supported on macOS")
}
