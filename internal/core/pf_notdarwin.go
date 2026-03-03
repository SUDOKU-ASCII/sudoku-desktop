//go:build !darwin

package core

func darwinBuildPFSetCmd(anchor string, tunIfExpr string, defaultIf string, gw4 string, gw6 string, tunIPv4 string, bypassV4File string, bypassV6File string, blockQUIC bool, dnsProxyPort int) string {
	return ""
}

func darwinBuildPFRestoreCmd(anchor string) string {
	return ""
}
