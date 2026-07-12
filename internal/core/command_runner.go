package core

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

func runCommand(logf func(string), name string, args ...string) error {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return reportCommandResult(logf, false, name, args, string(output), err)
}

func runCmdExec(logf func(string), name string, args ...string) error {
	return runCommand(logf, name, args...)
}

func runDarwinAdminCommand(logf func(string), name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	output, err := darwinAdminRunShLC(ctx, shellJoin(append([]string{name}, args...)...))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("run %s %s (admin): timeout", name, strings.Join(args, " "))
	}
	return reportCommandResult(logf, true, name, args, output, err)
}

func runCmdDarwinAdmin(logf func(string), name string, args ...string) error {
	return runDarwinAdminCommand(logf, name, args...)
}
func reportCommandResult(logf func(string), admin bool, name string, args []string, output string, err error) error {
	cleanOutput := strings.TrimSpace(output)
	command := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	logPrefix := "[route] "
	errorSuffix := ""
	if admin {
		logPrefix += "sudo "
		errorSuffix = " (admin)"
	}
	if logf != nil {
		if cleanOutput == "" {
			logf(logPrefix + command)
		} else {
			logf(fmt.Sprintf("%s%s => %s", logPrefix, command, cleanOutput))
		}
	}
	if err == nil {
		return nil
	}
	if cleanOutput == "" {
		return fmt.Errorf("run %s%s: %w", command, errorSuffix, err)
	}
	return fmt.Errorf("run %s%s: %w: %s", command, errorSuffix, err, cleanOutput)
}

func runCmdsDarwinAdmin(logf func(string), cmdlines ...string) error {
	if len(cmdlines) == 0 {
		return nil
	}
	shell := "set -e; " + strings.Join(cmdlines, "; ")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	output, err := darwinAdminRunShLC(ctx, shell)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("run (admin batch): timeout")
	}
	clean := strings.TrimSpace(output)
	if logf != nil {
		if clean == "" {
			logf("[route] sudo (batch)")
		} else {
			logf(fmt.Sprintf("[route] sudo (batch) => %s", clean))
		}
	}
	if err == nil {
		return nil
	}
	if clean == "" {
		return fmt.Errorf("run (admin batch): %w", err)
	}
	return fmt.Errorf("run (admin batch): %w: %s", err, clean)
}

func shellJoin(args ...string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$&;|<>*?()[]{}!") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
