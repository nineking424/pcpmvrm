package plan_test

import (
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestJobKind_String(t *testing.T) {
	cases := []struct {
		k    plan.JobKind
		want string
	}{
		{plan.JobCopy, "copy"},
		{plan.JobUnlink, "unlink"},
		{plan.JobDirCopy, "dir-copy"},
		{plan.JobDirRemove, "dir-remove"},
		{plan.JobRename, "rename"},
	}
	for _, c := range cases {
		if got := c.k.String(); got != c.want {
			t.Errorf("%d.String()=%q, want %q", c.k, got, c.want)
		}
	}
}
