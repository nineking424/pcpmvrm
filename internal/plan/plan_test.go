package plan_test

import (
	"testing"

	"github.com/nineking424/pcpmvrm/internal/plan"
)

func TestPlanValidate(t *testing.T) {
	tests := []struct {
		name    string
		p       plan.Plan
		wantErr string
	}{
		{
			name:    "ok minimum",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 1},
			wantErr: "",
		},
		{
			name:    "missing src",
			p:       plan.Plan{Op: plan.OpCopy, Dst: "/b", Workers: 1},
			wantErr: "src is required",
		},
		{
			name:    "missing dst for copy",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Workers: 1},
			wantErr: "dst is required for copy",
		},
		{
			name:    "missing dst for move",
			p:       plan.Plan{Op: plan.OpMove, Src: "/a", Workers: 1},
			wantErr: "dst is required for move",
		},
		{
			name:    "remove ok with empty dst",
			p:       plan.Plan{Op: plan.OpRemove, Src: "/a", Workers: 1},
			wantErr: "",
		},
		{
			name:    "workers must be positive",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 0},
			wantErr: "workers must be >= 1",
		},
		{
			name:    "strict-order and strict-extensions both allowed",
			p:       plan.Plan{Op: plan.OpCopy, Src: "/a", Dst: "/b", Workers: 1, StrictOrder: true, StrictExtensions: []string{".json"}},
			wantErr: "", // 둘 다 허용 (스펙 §5.2)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestPlan_Validate_OpMove(t *testing.T) {
	p := plan.Plan{Op: plan.OpMove, Src: "/a", Dst: "/b", Workers: 1}
	if err := p.Validate(); err != nil {
		t.Fatalf("OpMove plan should validate, got: %v", err)
	}
}

func TestPlan_SameDeviceField(t *testing.T) {
	p := plan.Plan{Op: plan.OpMove, Src: "/a", Dst: "/b", Workers: 1, SameDevice: true}
	if !p.SameDevice {
		t.Error("SameDevice field should be settable")
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("plan with SameDevice should validate: %v", err)
	}
}
