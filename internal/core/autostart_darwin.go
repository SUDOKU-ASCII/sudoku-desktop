//go:build darwin

package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	darwinLaunchAgentLabel    = "com.sudokuascii.sudoku4x4.autostart"
	darwinLaunchAgentFileName = "com.sudokuascii.sudoku4x4.autostart.plist"
)

func setLaunchAtLogin(enabled bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	agentsDir := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(agentsDir, darwinLaunchAgentFileName)

	if !enabled {
		if err := os.Remove(plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove launchagent: %w", err)
		}
		return nil
	}

	exe, err := currentExecutablePath()
	if err != nil {
		return err
	}
	appBundle := darwinResolveAppBundlePath(exe)

	args := make([]string, 0, 6)
	if appBundle != "" {
		// Prefer LaunchServices for packaged apps.
		args = append(args, "/usr/bin/open", "-a", appBundle, "--args", "--autostart")
	} else {
		args = append(args, exe, "--autostart")
	}

	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		return fmt.Errorf("create launchagents dir: %w", err)
	}
	content := darwinBuildLaunchAgentPlist(darwinLaunchAgentLabel, args)
	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write launchagent: %w", err)
	}
	return nil
}

func darwinResolveAppBundlePath(exe string) string {
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return ""
	}
	lower := strings.ToLower(exe)
	needle := ".app/contents/macos/"
	idx := strings.Index(lower, needle)
	if idx < 0 {
		return ""
	}
	// idx points to the beginning of ".app/Contents/MacOS/".
	end := idx + len(".app")
	if end <= 0 || end > len(exe) {
		return ""
	}
	return exe[:end]
}

func darwinBuildLaunchAgentPlist(label string, programArgs []string) string {
	if strings.TrimSpace(label) == "" {
		label = darwinLaunchAgentLabel
	}
	escaped := make([]string, 0, len(programArgs))
	for _, a := range programArgs {
		escaped = append(escaped, "<string>"+darwinXMLEscape(a)+"</string>")
	}

	// Keep this file minimal. launchd will pick it up at next login.
	return strings.Join([]string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">`,
		`<plist version="1.0">`,
		`<dict>`,
		`  <key>Label</key><string>` + darwinXMLEscape(label) + `</string>`,
		`  <key>RunAtLoad</key><true/>`,
		`  <key>KeepAlive</key><false/>`,
		`  <key>ProgramArguments</key>`,
		`  <array>`,
		`    ` + strings.Join(escaped, "\n    "),
		`  </array>`,
		`</dict>`,
		`</plist>`,
		"",
	}, "\n")
}

func darwinXMLEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
