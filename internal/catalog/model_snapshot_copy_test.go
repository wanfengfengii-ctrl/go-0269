package catalog

import (
	"slices"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

func TestModel_SnapshotFromRequestDoesNotAliasInputSlices(t *testing.T) {
	tests := []struct {
		name             string
		request          LockRequest
		wantCardSeals    []string
		wantBlindCodes   []string
		wantDayAges      []int
		wantSlideNumbers []string
	}{
		{
			name: "reverse ordered manifest",
			request: LockRequest{
				CardSeals:  []string{"seal-3", "seal-2", "seal-1"},
				BlindCodes: []string{"blind-3", "blind-2", "blind-1"},
				DayAges:    []int{3, 2, 1},
				SlideNos:   []string{"slide-3", "slide-2", "slide-1"},
			},
			wantCardSeals:    []string{"seal-1", "seal-2", "seal-3"},
			wantBlindCodes:   []string{"blind-1", "blind-2", "blind-3"},
			wantDayAges:      []int{1, 2, 3},
			wantSlideNumbers: []string{"slide-1", "slide-2", "slide-3"},
		},
		{
			name: "mixed ordered manifest",
			request: LockRequest{
				CardSeals:  []string{"seal-c", "seal-a", "seal-d", "seal-b"},
				BlindCodes: []string{"blind-b", "blind-d", "blind-a", "blind-c"},
				DayAges:    []int{5, 1, 3, 2},
				SlideNos:   []string{"slide-d", "slide-a", "slide-c", "slide-b"},
			},
			wantCardSeals:    []string{"seal-a", "seal-b", "seal-c", "seal-d"},
			wantBlindCodes:   []string{"blind-a", "blind-b", "blind-c", "blind-d"},
			wantDayAges:      []int{1, 2, 3, 5},
			wantSlideNumbers: []string{"slide-a", "slide-b", "slide-c", "slide-d"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalCardSeals := slices.Clone(tt.request.CardSeals)
			originalBlindCodes := slices.Clone(tt.request.BlindCodes)
			originalDayAges := slices.Clone(tt.request.DayAges)
			originalSlideNumbers := slices.Clone(tt.request.SlideNos)

			snapshot := SnapshotFromRequest(tt.request, domain.CatalogRule{RuleVersion: 7})

			if !slices.Equal(tt.request.CardSeals, originalCardSeals) {
				t.Fatalf("request CardSeals changed during snapshot creation: got %v, want %v", tt.request.CardSeals, originalCardSeals)
			}
			if !slices.Equal(snapshot.CardSeals, tt.wantCardSeals) {
				t.Errorf("snapshot CardSeals = %v, want %v", snapshot.CardSeals, tt.wantCardSeals)
			}
			if !slices.Equal(snapshot.BlindCodes, tt.wantBlindCodes) {
				t.Errorf("snapshot BlindCodes = %v, want %v", snapshot.BlindCodes, tt.wantBlindCodes)
			}
			if !slices.Equal(snapshot.DayAges, tt.wantDayAges) {
				t.Errorf("snapshot DayAges = %v, want %v", snapshot.DayAges, tt.wantDayAges)
			}
			if !slices.Equal(snapshot.SlideNos, tt.wantSlideNumbers) {
				t.Errorf("snapshot SlideNos = %v, want %v", snapshot.SlideNos, tt.wantSlideNumbers)
			}

			snapshot.CardSeals[0] = "changed-seal"
			snapshot.BlindCodes[0] = "changed-blind"
			snapshot.DayAges[0] = 99
			snapshot.SlideNos[0] = "changed-slide"
			if !slices.Equal(tt.request.CardSeals, originalCardSeals) {
				t.Errorf("request CardSeals shares snapshot storage: got %v, want %v", tt.request.CardSeals, originalCardSeals)
			}
			if !slices.Equal(tt.request.BlindCodes, originalBlindCodes) {
				t.Errorf("request BlindCodes shares snapshot storage: got %v, want %v", tt.request.BlindCodes, originalBlindCodes)
			}
			if !slices.Equal(tt.request.DayAges, originalDayAges) {
				t.Errorf("request DayAges shares snapshot storage: got %v, want %v", tt.request.DayAges, originalDayAges)
			}
			if !slices.Equal(tt.request.SlideNos, originalSlideNumbers) {
				t.Errorf("request SlideNos shares snapshot storage: got %v, want %v", tt.request.SlideNos, originalSlideNumbers)
			}
		})
	}
}
