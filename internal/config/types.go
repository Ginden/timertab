package config

type File struct {
	Schema  string `yaml:"$schema,omitempty"`
	Version int    `yaml:"version"`
	Jobs    []Job  `yaml:"jobs"`
}

type Job struct {
	ID        string            `yaml:"id,omitempty"`
	Name      string            `yaml:"name,omitempty"`
	When      ScheduleList      `yaml:"when"`
	Run       string            `yaml:"run"`
	Env       map[string]string `yaml:"env,omitempty"`
	Cwd       string            `yaml:"cwd,omitempty"`
	Enabled   *bool             `yaml:"enabled,omitempty"`
	OnSuccess *Hook             `yaml:"on_success,omitempty"`
	OnFailure *Hook             `yaml:"on_failure,omitempty"`
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
