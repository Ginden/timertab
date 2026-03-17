package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const DefaultTemplate = `$schema: "https://raw.githubusercontent.com/ginden/timertab/v1.0.0/schema/v1.json"
version: 1
jobs:
  - name: "example"
    when: "@daily"
    run: |-
      echo 'string run values use /bin/sh -lc'
      echo 'use run: ["/usr/bin/env", "bash", "-lc", "echo ok"] for direct argv mode'
`

func LoadFromFile(path string) (*File, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return LoadFromBytes(buf)
}

func LoadFromBytes(buf []byte) (*File, error) {
	var raw any
	rawDecoder := yaml.NewDecoder(bytes.NewReader(buf))
	if err := rawDecoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if err := validateConfigSchema(raw); err != nil {
		return nil, err
	}

	var f File
	decoder := yaml.NewDecoder(bytes.NewReader(buf))
	decoder.KnownFields(true)
	if err := decoder.Decode(&f); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	if err := f.Validate(); err != nil {
		return nil, err
	}

	return &f, nil
}

func (f *File) MarshalYAML() ([]byte, error) {
	out, err := yaml.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("marshal yaml: %w", err)
	}
	return out, nil
}
