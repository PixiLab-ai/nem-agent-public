package peek

import (
	"os"
	"testing"
	"time"
)

// TestLiveFront samples this computer for real: NEM_PEEK_LIVE=1 go test -run TestLiveFront -v.
// It prints only what Peek keeps (the mode and a category), never an app's name.
func TestLiveFront(t *testing.T) {
	if os.Getenv("NEM_PEEK_LIVE") == "" {
		t.Skip("set NEM_PEEK_LIVE=1 to sample this computer")
	}
	s, mode := NewSampler()
	defer s.Close()
	for i := 0; i < 3; i++ {
		f, err := s.Front()
		if err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
		app := Categorize(f)
		t.Logf("mode=%s category=%s quiet=%v named_game=%v", mode, app.Category, f.Quiet, app.Game != "")
		if steamTools.MatchString(app.Game) {
			t.Errorf("a Steam tool taken for a game")
		}
		time.Sleep(time.Second)
	}
}
