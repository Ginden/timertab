package config

import (
"fmt"
"testing"
)

func TestSpecificCronPatterns(t *testing.T) {
patterns := []struct {
name     string
schedule string
}{
{"@reboot", "@reboot"},
{"step */15", "*/15 * * * *"},
{"complex range", "1,3,5-7 * * * *"},
{"weekday range", "0 9 * * 1-5"},
{"yearly", "0 0 1 1 *"},
{"mixed step + range", "*/7 0-12 * * *"},
}

for _, p := range patterns {
t.Run(p.name, func(t *testing.T) {
directives, err := CompileTimerDirectives(ScheduleList{p.schedule})
if err != nil {
t.Fatalf("Error: %v", err)
}
fmt.Printf("%-25s -> %v\n", p.schedule, directives)
})
}
}
