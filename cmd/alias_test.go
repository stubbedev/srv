package cmd

import "testing"

func TestNormalizeAliases(t *testing.T) {
	tests := []struct {
		name      string
		canonical string
		input     []string
		want      []string
		wantErr   bool
	}{
		{
			name:      "empty input",
			canonical: "a.test",
			input:     nil,
			want:      []string{},
		},
		{
			name:      "lowercases and dedupes",
			canonical: "a.test",
			input:     []string{"B.test", "b.test", "c.test"},
			want:      []string{"b.test", "c.test"},
		},
		{
			name:      "rejects canonical clash",
			canonical: "a.test",
			input:     []string{"A.TEST"},
			want:      []string{},
		},
		{
			name:      "skips empty entries",
			canonical: "a.test",
			input:     []string{"", "b.test", "   "},
			want:      []string{"b.test"},
		},
		{
			name:      "rejects invalid alias",
			canonical: "a.test",
			input:     []string{"not valid"},
			wantErr:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeAliases(tc.canonical, tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i, v := range got {
				if v != tc.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, v, tc.want[i])
				}
			}
		})
	}
}
