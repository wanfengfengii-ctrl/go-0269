package app

import (
	"context"
	"testing"

	"silkworm-egg-cold-storage-gate/internal/domain"
)

func TestModel_SubmitObservationsIsAtomic(t *testing.T) {
	validCells := []domain.ObservationCell{
		{CardSeal: "seal-1", DayAge: 1, Hatched: 80, Unhatched: 15, Dead: 4, Damaged: 1},
		{CardSeal: "seal-1", DayAge: 2, Hatched: 82, Unhatched: 13, Dead: 4, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 1, Hatched: 81, Unhatched: 14, Dead: 4, Damaged: 1},
		{CardSeal: "seal-2", DayAge: 2, Hatched: 83, Unhatched: 12, Dead: 4, Damaged: 1},
	}
	tests := []struct {
		name          string
		cells         []domain.ObservationCell
		wantErrCode   domain.ErrorCode
		wantReason    string
		wantStatus    int
		wantCellCount int
		wantGapCount  int
		wantComplete  bool
		wantState     string
	}{
		{
			name: "later conservation error rolls back earlier cells",
			cells: []domain.ObservationCell{
				validCells[0],
				{CardSeal: "seal-1", DayAge: 2, Hatched: 50, Unhatched: 20, Dead: 4, Damaged: 1},
			},
			wantErrCode:   domain.ErrInvalidInput,
			wantReason:    "CONSERVATION_VIOLATION",
			wantCellCount: 0,
			wantGapCount:  4,
			wantComplete:  false,
			wantState:     "OBSERVING_HATCHING",
		},
		{
			name:          "valid full batch commits together and advances",
			cells:         validCells,
			wantStatus:    200,
			wantCellCount: 4,
			wantGapCount:  0,
			wantComplete:  true,
			wantState:     "VERIFYING_MICROSCOPY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService(t, nil, nil)
			id := createAndLock(t, svc)
			driveToObservation(t, svc, id)
			ctx := context.Background()

			status, body, err := svc.SubmitObservations(ctx, "op-observations-"+id, id, ObservationRequest{
				Generation: 1,
				Cells:      tt.cells,
			})
			if tt.wantErrCode != "" {
				de, ok := err.(*domain.DomainError)
				if !ok {
					t.Fatalf("SubmitObservations error = %T %v, want *domain.DomainError", err, err)
				}
				if de.Code != tt.wantErrCode {
					t.Fatalf("SubmitObservations error code = %q, want %q", de.Code, tt.wantErrCode)
				}
				if len(de.Reasons) != 1 || de.Reasons[0].Code != tt.wantReason {
					t.Fatalf("SubmitObservations reasons = %#v, want one %q reason", de.Reasons, tt.wantReason)
				}
				if status != 0 || body != nil {
					t.Fatalf("failed SubmitObservations returned status/body = %d/%#v, want 0/nil", status, body)
				}
			} else {
				if err != nil {
					t.Fatalf("SubmitObservations error = %v", err)
				}
				if status != tt.wantStatus {
					t.Fatalf("SubmitObservations status = %d, want %d", status, tt.wantStatus)
				}
			}

			_, coverageBody, err := svc.GetCoverage(ctx, id)
			if err != nil {
				t.Fatalf("GetCoverage error = %v", err)
			}
			coverage := coverageBody.(map[string]any)
			if cells := coverage["cells"].([]map[string]any); len(cells) != tt.wantCellCount {
				t.Fatalf("committed coverage cells = %d, want %d", len(cells), tt.wantCellCount)
			}
			if gaps := coverage["gaps"].([]map[string]any); len(gaps) != tt.wantGapCount {
				t.Fatalf("coverage gaps = %d, want %d", len(gaps), tt.wantGapCount)
			}
			if complete := coverage["complete"].(bool); complete != tt.wantComplete {
				t.Fatalf("coverage complete = %v, want %v", complete, tt.wantComplete)
			}

			_, taskBody, err := svc.GetTask(ctx, id)
			if err != nil {
				t.Fatalf("GetTask error = %v", err)
			}
			if state := taskBody.(map[string]any)["state"]; state != tt.wantState {
				t.Fatalf("task state = %v, want %s", state, tt.wantState)
			}
		})
	}
}
