//go:build darwin

package core

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestDarwinAdminRunShLCExecutesPrivilegedScriptOnce(t *testing.T) {
	oldRunner := darwinRunSudoCommand
	oldPassword, hadPassword := darwinAdminPasswordCopy()
	defer restoreDarwinSudoTestState(oldRunner, oldPassword, hadPassword)

	darwinSudo.mu.Lock()
	zeroBytes(darwinSudo.password)
	darwinSudo.password = []byte("correct-password")
	darwinSudo.mu.Unlock()

	calls := 0
	var gotStdin []byte
	var gotArgs []string
	wantErr := errors.New("script failed")
	darwinRunSudoCommand = func(_ context.Context, stdin []byte, args ...string) (string, error) {
		calls++
		gotStdin = append([]byte(nil), stdin...)
		gotArgs = append([]string(nil), args...)
		return "", wantErr
	}

	_, err := darwinAdminRunShLC(context.Background(), "exit 23")
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("privileged script executed %d times, want 1", calls)
	}
	if string(gotStdin) != "correct-password\n" {
		t.Fatalf("unexpected sudo stdin: %q", gotStdin)
	}
	wantArgs := []string{"-S", "-p", "", "--", darwinShPath, "-lc", darwinAdminNormalizeScript("exit 23")}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("unexpected sudo args:\n got: %#v\nwant: %#v", gotArgs, wantArgs)
	}
}

func restoreDarwinSudoTestState(runner func(context.Context, []byte, ...string) (string, error), password []byte, hadPassword bool) {
	darwinRunSudoCommand = runner
	darwinSudo.mu.Lock()
	defer darwinSudo.mu.Unlock()
	zeroBytes(darwinSudo.password)
	if hadPassword {
		darwinSudo.password = password
	} else {
		zeroBytes(password)
		darwinSudo.password = nil
	}
}
