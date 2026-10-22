//go:build windows

package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	windowsInternetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	windowsWinHTTPKey          = `SOFTWARE\Microsoft\Windows\CurrentVersion\Internet Settings\Connections`
	windowsWinHTTPValue        = "WinHttpSettings"
)

func platformCaptureSystemProxy(context.Context) (*systemProxySnapshot, error) {
	snap, err := windowsReadProxySnapshot()
	if err != nil {
		return nil, err
	}
	if strings.Contains(snap.ProxyServer, fmt.Sprintf("127.0.0.1:%d", ProxyPort)) {
		// Left over from a crashed run; not the user's configuration.
		snap.ProxyEnable, snap.ProxyServer = 0, ""
	}
	if raw, ok := windowsReadWinHTTP(); ok && !windowsWinHTTPIsOurs(raw, ProxyPort) {
		snap.WinHTTP = raw
	}
	return &systemProxySnapshot{Windows: &snap}, nil
}

func platformSetSystemProxy(_ context.Context, base *systemProxySnapshot, port int, logf func(string)) error {
	if base == nil || base.Windows == nil {
		return errors.New("no proxy snapshot")
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	override := uniqueStrings(append(strings.Split(base.Windows.ProxyOverride, ";"), append([]string{"<local>"}, proxyBypassLocal...)...))
	if err := windowsWriteProxySettings(windowsProxySnapshot{
		ProxyEnable:   1,
		ProxyServer:   fmt.Sprintf("http=%s;https=%s;socks=%s", addr, addr, addr),
		ProxyOverride: strings.Join(override, ";"),
	}); err != nil {
		return err
	}
	_ = windowsRefreshInternetSettings()
	windowsSetWinHTTP(port, logf)
	return nil
}

func platformRestoreSystemProxy(_ context.Context, snap *systemProxySnapshot, port int, logf func(string)) error {
	if snap.Windows == nil {
		return nil
	}
	windowsRestoreWinHTTP(snap.Windows.WinHTTP, port, logf)
	cur, err := windowsReadProxySnapshot()
	if err != nil {
		return err
	}
	if !strings.Contains(cur.ProxyServer, fmt.Sprintf("127.0.0.1:%d", port)) {
		return nil
	}
	if err := windowsWriteProxySettings(*snap.Windows); err != nil {
		return err
	}
	_ = windowsRefreshInternetSettings()
	return nil
}

// WinHTTP is the proxy of services and many CLI tools; it is machine-wide and needs
// administrator rights, so it is synced only when the app itself runs elevated.

func windowsElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

func windowsReadWinHTTP() ([]byte, bool) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, windowsWinHTTPKey, registry.QUERY_VALUE)
	if err != nil {
		return nil, false
	}
	defer key.Close()
	raw, _, err := key.GetBinaryValue(windowsWinHTTPValue)
	if err != nil {
		return nil, false
	}
	return raw, true
}

func windowsWinHTTPIsOurs(raw []byte, port int) bool {
	return bytes.Contains(raw, []byte(fmt.Sprintf("127.0.0.1:%d", port)))
}

func windowsSetWinHTTP(port int, logf func(string)) {
	if !windowsElevated() {
		return
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cmd := exec.Command("netsh", "winhttp", "set", "proxy", "proxy-server="+addr, "bypass-list=<local>;localhost;127.0.0.1;::1")
	applyManagedProcessSysProcAttr(cmd)
	if out, err := cmd.CombinedOutput(); err != nil && logf != nil {
		logf(fmt.Sprintf("set winhttp proxy: %v: %s", err, strings.TrimSpace(string(out))))
	}
}

// windowsRestoreWinHTTP puts the saved value back if WinHTTP still points at us.
func windowsRestoreWinHTTP(saved []byte, port int, logf func(string)) {
	cur, ok := windowsReadWinHTTP()
	if !ok || !windowsWinHTTPIsOurs(cur, port) || !windowsElevated() {
		return
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, windowsWinHTTPKey, registry.SET_VALUE)
	if err == nil {
		if len(saved) > 0 {
			err = key.SetBinaryValue(windowsWinHTTPValue, saved)
		} else {
			err = key.DeleteValue(windowsWinHTTPValue)
		}
		key.Close()
	}
	if err != nil && logf != nil {
		logf("restore winhttp proxy: " + err.Error())
	}
}

func platformSystemProxyIsOurs(_ context.Context, port int) bool {
	cur, err := windowsReadProxySnapshot()
	return err == nil && cur.ProxyEnable == 1 && strings.Contains(cur.ProxyServer, fmt.Sprintf("127.0.0.1:%d", port))
}

func windowsReadProxySnapshot() (windowsProxySnapshot, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsInternetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return windowsProxySnapshot{}, err
	}
	defer key.Close()
	enable, _, _ := key.GetIntegerValue("ProxyEnable")
	autoDetect, _, _ := key.GetIntegerValue("AutoDetect")
	server, _, _ := key.GetStringValue("ProxyServer")
	autoURL, _, _ := key.GetStringValue("AutoConfigURL")
	override, _, _ := key.GetStringValue("ProxyOverride")
	return windowsProxySnapshot{
		ProxyEnable: uint32(enable), AutoDetect: uint32(autoDetect),
		ProxyServer: server, AutoConfigURL: autoURL, ProxyOverride: override,
	}, nil
}

func windowsWriteProxySettings(s windowsProxySnapshot) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsInternetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	if err := key.SetDWordValue("ProxyEnable", s.ProxyEnable); err != nil {
		return err
	}
	if err := key.SetDWordValue("AutoDetect", s.AutoDetect); err != nil {
		return err
	}
	if err := key.SetStringValue("ProxyServer", s.ProxyServer); err != nil {
		return err
	}
	if strings.TrimSpace(s.AutoConfigURL) == "" {
		_ = key.DeleteValue("AutoConfigURL")
	} else if err := key.SetStringValue("AutoConfigURL", s.AutoConfigURL); err != nil {
		return err
	}
	return key.SetStringValue("ProxyOverride", s.ProxyOverride)
}

func windowsRefreshInternetSettings() error {
	const (
		internetOptionSettingsChanged = 39
		internetOptionRefresh         = 37
	)
	proc := windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")
	for _, opt := range []uintptr{internetOptionSettingsChanged, internetOptionRefresh} {
		if r1, _, e1 := proc.Call(0, opt, 0, 0); r1 == 0 {
			if e1 != nil && e1 != windows.ERROR_SUCCESS {
				return e1
			}
			return errors.New("InternetSetOptionW failed")
		}
	}
	return nil
}
