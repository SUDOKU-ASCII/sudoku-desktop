//go:build windows

package core

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type windowsProxySnapshot struct {
	ProxyEnable   uint32
	ProxyServer   string
	AutoConfigURL string
	ProxyOverride string
}

func platformApplySystemProxy(cfg systemProxyConfig) (func() error, error) {
	snap, err := windowsReadProxySnapshot()
	if err != nil {
		return nil, err
	}

	applyErr := func() error {
		if cfg.ProxyMode == "pac" && strings.TrimSpace(cfg.PACURL) != "" {
			if err := windowsWriteProxySettings(windowsProxySnapshot{
				ProxyEnable:   0,
				ProxyServer:   "",
				AutoConfigURL: strings.TrimSpace(cfg.PACURL),
				ProxyOverride: snap.ProxyOverride,
			}); err != nil {
				return err
			}
			_ = windowsRefreshInternetSettings()
			return nil
		}

		if cfg.LocalPort <= 0 || cfg.LocalPort > 65535 {
			return errors.New("invalid local port")
		}
		addr := fmt.Sprintf("127.0.0.1:%d", cfg.LocalPort)
		server := fmt.Sprintf("http=%s;https=%s;socks=%s", addr, addr, addr)

		override := uniqueStrings(strings.Split(snap.ProxyOverride, ";"))
		override = uniqueStrings(append(override, "<local>", "localhost", "127.0.0.1", "::1"))

		if err := windowsWriteProxySettings(windowsProxySnapshot{
			ProxyEnable:   1,
			ProxyServer:   server,
			AutoConfigURL: "",
			ProxyOverride: strings.Join(override, ";"),
		}); err != nil {
			return err
		}
		_ = windowsRefreshInternetSettings()
		return nil
	}()
	if applyErr != nil {
		return nil, applyErr
	}

	return func() error {
		if err := windowsWriteProxySettings(snap); err != nil {
			return err
		}
		_ = windowsRefreshInternetSettings()
		return nil
	}, nil
}

func windowsReadProxySnapshot() (windowsProxySnapshot, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return windowsProxySnapshot{}, err
	}
	defer key.Close()

	enable := uint64(0)
	if v, _, err := key.GetIntegerValue("ProxyEnable"); err == nil {
		enable = v
	}
	server, _, _ := key.GetStringValue("ProxyServer")
	autoURL, _, _ := key.GetStringValue("AutoConfigURL")
	override, _, _ := key.GetStringValue("ProxyOverride")

	return windowsProxySnapshot{
		ProxyEnable:   uint32(enable),
		ProxyServer:   server,
		AutoConfigURL: autoURL,
		ProxyOverride: override,
	}, nil
}

func windowsWriteProxySettings(s windowsProxySnapshot) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if err := key.SetDWordValue("ProxyEnable", s.ProxyEnable); err != nil {
		return err
	}
	if err := key.SetStringValue("ProxyServer", s.ProxyServer); err != nil {
		return err
	}
	if err := key.SetStringValue("AutoConfigURL", s.AutoConfigURL); err != nil {
		return err
	}
	if err := key.SetStringValue("ProxyOverride", s.ProxyOverride); err != nil {
		return err
	}
	return nil
}

func windowsRefreshInternetSettings() error {
	const (
		internetOptionSettingsChanged = 39
		internetOptionRefresh         = 37
	)

	wininet := windows.NewLazySystemDLL("wininet.dll")
	proc := wininet.NewProc("InternetSetOptionW")

	call := func(opt uintptr) error {
		r1, _, e1 := proc.Call(0, opt, 0, 0)
		if r1 == 0 {
			if e1 != nil && e1 != windows.ERROR_SUCCESS {
				return e1
			}
			return errors.New("InternetSetOptionW failed")
		}
		return nil
	}

	if err := call(internetOptionSettingsChanged); err != nil {
		return err
	}
	if err := call(internetOptionRefresh); err != nil {
		return err
	}
	return nil
}
