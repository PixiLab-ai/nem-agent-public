package peek

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeSampler struct{ f *Front }

func (s fakeSampler) Front() (Front, error) { return *s.f, nil }
func (fakeSampler) Close()                  {}

type sent struct {
	r  Report
	ev *Event
}

// a Peek on a fake clock and a fake screen; look() is one sample 20 s later
func newTestPeek(t *testing.T, front *Front) (*Peek, *time.Time, *[]sent, func()) {
	t.Helper()
	now := at(9, 0)
	var got []sent
	p := New(Options{
		StatsFile:  filepath.Join(t.TempDir(), "peek.json"),
		Report:     func(r Report, ev *Event) { got = append(got, sent{r, ev}) },
		NewSampler: func() (Sampler, string) { return fakeSampler{front}, ModeFront },
		Now:        func() time.Time { return now },
	})
	look := func() {
		now = now.Add(20 * time.Second)
		ev, r := p.sample()
		if r != nil {
			p.o.Report(*r, ev)
		}
	}
	return p, &now, &got, look
}

func TestReportsWhatThePetHears(t *testing.T) {
	game := &Front{Exe: "/mnt/games/SteamLibrary/steamapps/common/ELDEN RING/Game/eldenring"}
	p, _, got, look := newTestPeek(t, game)
	look()
	if len(*got) != 0 {
		t.Fatalf("reported while off: %+v", *got)
	}
	p.SetOn(true)
	look()
	if len(*got) != 1 || (*got)[0].r.Today[Gaming] != 0 && (*got)[0].r.Now == nil {
		t.Fatalf("first look: %+v", *got)
	}
	// then quiet for a while: a report every 5 minutes, not every look
	for i := 0; i < 3*15; i++ { // 15 minutes
		look()
	}
	if n := len(*got); n < 3 || n > 5 {
		t.Fatalf("%d reports in 15 minutes", n)
	}
	last := (*got)[len(*got)-1].r
	if last.Now == nil || last.Now.Category != Gaming || last.Now.Minutes < 10 || last.Today[Gaming] < 10 {
		t.Fatalf("last report: %+v %+v", last.Now, last.Today)
	}
	// two hours in: the moment, with the game's name for the app's own line only
	for i := 0; i < 3*110; i++ {
		look()
	}
	var moment *sent
	for i := range *got {
		if (*got)[i].ev != nil {
			moment = &(*got)[i]
		}
	}
	if moment == nil || moment.r.Event == nil || moment.r.Event.Kind != "long" || moment.ev.Game != "ELDEN RING" {
		t.Fatalf("no long moment: %+v", moment)
	}
	for _, s := range *got {
		raw, _ := json.Marshal(s.r)
		if strings.Contains(strings.ToLower(string(raw)), "elden") || strings.Contains(string(raw), "steamapps") {
			t.Fatalf("a name went up: %s", raw)
		}
	}
	// full screen: told at once
	before := len(*got)
	game.Quiet = true
	look()
	if len(*got) != before+1 || !(*got)[before].r.Now.Quiet {
		t.Fatalf("full screen not told: %+v", (*got)[before:])
	}
	// off, pause, forget: the pet forgets now
	for _, act := range []func(){func() { p.Pause(time.Hour) }, func() { _ = p.Forget() }, func() { p.SetOn(false) }} {
		n := len(*got)
		act()
		if len(*got) != n+1 || !(*got)[n].r.Off {
			t.Fatalf("no off: %+v", (*got)[n:])
		}
	}
	look()
	if r, _, on := p.Judge(); on || r.Event == nil || !r.Event.Asked {
		t.Fatalf("judge while off: on=%v %+v", on, r.Event)
	}
}
