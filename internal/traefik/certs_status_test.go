package traefik

import (
	"testing"

	"github.com/stubbedev/srv/internal/constants"
)

func TestCertInfoStatus(t *testing.T) {
	cases := []struct {
		name string
		in   CertInfo
		want CertStatus
	}{
		{
			name: "corrupt takes precedence over everything",
			in:   CertInfo{Corrupt: true, Exists: true, DaysLeft: 100},
			want: CertStatusCorrupt,
		},
		{
			name: "missing when not present",
			in:   CertInfo{Exists: false},
			want: CertStatusMissing,
		},
		{
			name: "expired",
			in:   CertInfo{Exists: true, IsExpired: true, DaysLeft: -3},
			want: CertStatusExpired,
		},
		{
			name: "expiring at the warning threshold",
			in:   CertInfo{Exists: true, DaysLeft: constants.CertExpiryWarningDays},
			want: CertStatusExpiring,
		},
		{
			name: "expiring just below the threshold",
			in:   CertInfo{Exists: true, DaysLeft: constants.CertExpiryWarningDays - 1},
			want: CertStatusExpiring,
		},
		{
			name: "valid well beyond the threshold",
			in:   CertInfo{Exists: true, DaysLeft: constants.CertExpiryWarningDays + 1},
			want: CertStatusValid,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Status(); got != tc.want {
				t.Errorf("Status() = %q, want %q", got, tc.want)
			}
		})
	}
}
