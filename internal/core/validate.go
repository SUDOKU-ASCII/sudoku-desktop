package core

import (
	"strings"

	"gopkg.in/yaml.v3"
)

func (b *Backend) ValidateYAML(content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	var v any
	return yaml.Unmarshal([]byte(content), &v)
}
