package logger

import "testing"

func TestResolveLogOutput(t *testing.T) {
	tests := []struct {
		name                   string
		transport              string
		configuredOutput       string
		explicitStdoutOverride bool
		wantEffective          string
		wantWarning            bool
	}{
		{
			name:                   "stdio with explicit stdout override forces stderr and warns",
			transport:              "stdio",
			configuredOutput:       "stdout",
			explicitStdoutOverride: true,
			wantEffective:          "stderr",
			wantWarning:            true,
		},
		{
			name:                   "stdio with default stdout forces stderr without warning",
			transport:              "stdio",
			configuredOutput:       "stdout",
			explicitStdoutOverride: false,
			wantEffective:          "stderr",
			wantWarning:            false,
		},
		{
			name:                   "stdio already configured for stderr is unaffected",
			transport:              "stdio",
			configuredOutput:       "stderr",
			explicitStdoutOverride: false,
			wantEffective:          "stderr",
			wantWarning:            false,
		},
		{
			name:                   "stdio configured for file is unaffected",
			transport:              "stdio",
			configuredOutput:       "file",
			explicitStdoutOverride: false,
			wantEffective:          "file",
			wantWarning:            false,
		},
		{
			name:                   "http with explicit stdout is unaffected",
			transport:              "http",
			configuredOutput:       "stdout",
			explicitStdoutOverride: true,
			wantEffective:          "stdout",
			wantWarning:            false,
		},
		{
			name:                   "http with stderr is unaffected",
			transport:              "http",
			configuredOutput:       "stderr",
			explicitStdoutOverride: false,
			wantEffective:          "stderr",
			wantWarning:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			effective, warning := resolveLogOutput(tt.transport, tt.configuredOutput, tt.explicitStdoutOverride)
			if effective != tt.wantEffective {
				t.Errorf("resolveLogOutput(%q, %q, %v) effective = %q, want %q",
					tt.transport, tt.configuredOutput, tt.explicitStdoutOverride, effective, tt.wantEffective)
			}
			if (warning != "") != tt.wantWarning {
				t.Errorf("resolveLogOutput(%q, %q, %v) warning = %q, want non-empty=%v",
					tt.transport, tt.configuredOutput, tt.explicitStdoutOverride, warning, tt.wantWarning)
			}
		})
	}
}
