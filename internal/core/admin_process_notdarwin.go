//go:build !darwin

package core

func newAdminDetachedProcess() adminDetachedProcess {
	return nil
}
