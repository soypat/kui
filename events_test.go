package kui

import "testing"

func TestKeyModsAppendString(t *testing.T) {
	tests := []struct {
		mods KeyMods
		want string
	}{
		{0, ""},
		{KeyModShift, "shift"},
		{KeyModCtrl, "ctrl"},
		{KeyModCtrl | KeyModShift, "shift ctrl"},
		{KeyModAlt | KeyModSuper | KeyModNumLock, "alt super numL"},
	}
	for _, tc := range tests {
		if got := string(tc.mods.AppendString(nil)); got != tc.want {
			t.Errorf("%#x: want %q, got %q", uint8(tc.mods), tc.want, got)
		}
	}
}
