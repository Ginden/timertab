package cli

import (
	"errors"
)

type Options struct {
	List      bool
	Edit      bool
	User      string
	Config    string
	NoApply   bool
	PrintPath bool
}

func (o *Options) Validate() error {
	if o.List && o.Edit {
		return errors.New("flags -l/--print-config and -e are mutually exclusive")
	}

	if o.NoApply && !o.Edit {
		return errors.New("--no-apply can only be used with -e")
	}

	if o.PrintPath && (o.List || o.Edit) {
		return errors.New("--print-path cannot be combined with -l/--print-config or -e")
	}

	return nil
}
