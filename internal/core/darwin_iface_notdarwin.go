//go:build !darwin

package core

func darwinInterfaceIPv4(_ string) string {
	return ""
}
