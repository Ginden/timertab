package config

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

type File struct {
	Schema     string `yaml:"$schema,omitempty"`
	Version    int    `yaml:"version"`
	InstanceID string `yaml:"instance_id,omitempty"`
	Git        *Git   `yaml:"git,omitempty"`
	Jobs       []Job  `yaml:"jobs"`
}

type Git struct {
	AutoCommit *bool `yaml:"auto_commit,omitempty"`
}

type Job struct {
	ID         string            `yaml:"id,omitempty"`
	Name       string            `yaml:"name,omitempty"`
	When       ScheduleList      `yaml:"when"`
	Run        string            `yaml:"run"`
	Env        map[string]string `yaml:"env,omitempty"`
	Cwd        string            `yaml:"cwd,omitempty"`
	Enabled    *bool             `yaml:"enabled,omitempty"`
	Persistent *bool             `yaml:"persistent,omitempty"`
	Jitter     string            `yaml:"jitter,omitempty"`
	Limits     *Limits           `yaml:"limits,omitempty"`
	Systemd    *Systemd          `yaml:"systemd,omitempty"`
	OnSuccess  *Hook             `yaml:"on_success,omitempty"`
	OnFailure  *Hook             `yaml:"on_failure,omitempty"`
}

type Limits struct {
	MemoryMax string `yaml:"MemoryMax,omitempty"`
	CPUQuota  string `yaml:"CPUQuota,omitempty"`
	IOWeight  *int   `yaml:"IOWeight,omitempty"`
}

type Systemd struct {
	Service *SystemdDirectiveSet `yaml:"service,omitempty"`
	Timer   *SystemdDirectiveSet `yaml:"timer,omitempty"`
}

type SystemdDirectiveSet struct {
	Map   map[string]string  `yaml:"-"`
	Items []SystemdDirective `yaml:"-"`
}

type SystemdDirective struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
}

func (s *SystemdDirectiveSet) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.MappingNode:
		var mapped map[string]string
		if err := value.Decode(&mapped); err != nil {
			return err
		}
		s.Map = mapped
		s.Items = nil
		return nil
	case yaml.SequenceNode:
		var items []SystemdDirective
		if err := value.Decode(&items); err != nil {
			return err
		}
		s.Items = items
		s.Map = nil
		return nil
	default:
		return fmt.Errorf("must be a mapping or array")
	}
}

func (s SystemdDirectiveSet) MarshalYAML() (any, error) {
	if len(s.Items) > 0 {
		return s.Items, nil
	}
	if s.Map != nil {
		return s.Map, nil
	}
	return nil, nil
}

func (s *SystemdDirectiveSet) Directives() []SystemdDirective {
	if s == nil {
		return nil
	}
	if len(s.Items) > 0 {
		out := make([]SystemdDirective, len(s.Items))
		copy(out, s.Items)
		return out
	}
	if len(s.Map) == 0 {
		return nil
	}

	keys := make([]string, 0, len(s.Map))
	for key := range s.Map {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]SystemdDirective, 0, len(keys))
	for _, key := range keys {
		out = append(out, SystemdDirective{
			Name:  key,
			Value: s.Map[key],
		})
	}
	return out
}

type Hook struct {
	Command string            `yaml:"command"`
	Env     map[string]string `yaml:"env,omitempty"`
}

func (j Job) IsEnabled() bool {
	if j.Enabled == nil {
		return true
	}
	return *j.Enabled
}

func (f *File) AutoCommitEnabled() bool {
	if f == nil || f.Git == nil || f.Git.AutoCommit == nil {
		return true
	}
	return *f.Git.AutoCommit
}

func (f *File) EffectiveInstanceID() string {
	if f == nil {
		return DefaultInstanceID
	}
	if f.InstanceID == "" {
		return DefaultInstanceID
	}
	return f.InstanceID
}
