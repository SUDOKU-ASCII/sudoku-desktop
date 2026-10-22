//go:build !darwin

package core

func (h *helperServer) wakeLoop(*tunSession) {}
