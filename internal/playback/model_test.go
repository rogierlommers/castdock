package playback

import "testing"

func TestStateValidate(t *testing.T) {
	tests := []struct {
		name    string
		state   State
		wantErr bool
	}{
		{name: "valid", state: State{EpisodeID: 1, Position: 10, Duration: 30}},
		{name: "unknown duration", state: State{EpisodeID: 1, Position: 10}},
		{name: "bad episode", state: State{Position: 10}, wantErr: true},
		{name: "negative", state: State{EpisodeID: 1, Position: -1}, wantErr: true},
		{name: "past duration", state: State{EpisodeID: 1, Position: 40, Duration: 30}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.Validate(); (got != nil) != tt.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", got, tt.wantErr)
			}
		})
	}
}
