package cli_test

import (
	"reflect"
	"testing"

	"github.com/nineking424/pcpmvrm/internal/cli"
)

func TestParseCommon(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want cli.Common
	}{
		{
			name: "defaults",
			args: []string{},
			want: cli.Common{Workers: 1},
		},
		{
			name: "all set",
			args: []string{
				"--parallel=8",
				"--strict-order",
				"--strict-extension=.json,.csv",
				"--exit-on-error",
				"--error-log=/tmp/x.log",
				"--dry-run",
				"--no-progress",
			},
			want: cli.Common{
				Workers:          8,
				StrictOrder:      true,
				StrictExtensions: []string{".json", ".csv"},
				ExitOnError:      true,
				ErrorLogPath:     "/tmp/x.log",
				DryRun:           true,
				NoProgress:       true,
			},
		},
		{
			name: "extension normalization",
			args: []string{"--strict-extension=JSON,csv,.png"},
			want: cli.Common{Workers: 1, StrictExtensions: []string{".json", ".csv", ".png"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := cli.ParseCommon(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
