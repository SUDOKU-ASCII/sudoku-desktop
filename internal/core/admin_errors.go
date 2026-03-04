package core

import "errors"

// ErrAdminRequired indicates an operation requires administrator privileges.
//
// On macOS, this is used by the in-app TUN privilege flow to signal that the
// user must provide their macOS login password before we can modify routes/DNS.
var ErrAdminRequired = errors.New("administrator privileges required")
