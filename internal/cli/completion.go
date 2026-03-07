package cli

import (
	"errors"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ginden/timertab/internal/config"
)

func completeJobIDs(overridePath, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfgPath, err := resolveConfigPath(overridePath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	loaded, err := config.LoadFromFile(cfgPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	if err := loaded.NormalizeIDs(); err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	matches := make([]string, 0, len(loaded.Jobs))
	for _, job := range loaded.Jobs {
		if strings.HasPrefix(job.ID, toComplete) {
			matches = append(matches, job.ID)
		}
	}

	sort.Strings(matches)
	return matches, cobra.ShellCompDirectiveNoFileComp
}
