package peek

import (
	"testing"
	"time"
)

// a day of samples every 20 s, from start for d, of one app
func feed(r *Rules, start time.Time, d time.Duration, app App) []Event {
	var out []Event
	for t := start; t.Before(start.Add(d)); t = t.Add(20 * time.Second) {
		if ev := r.Observe(t, app, nil); ev != nil {
			out = append(out, *ev)
		}
	}
	return out
}

func at(h, m int) time.Time { return time.Date(2026, 10, 20, h, m, 0, 0, time.Local) } // a Tuesday

func TestMorningThenLongCoding(t *testing.T) {
	var r Rules
	evs := feed(&r, at(9, 0), 2*time.Hour+30*time.Minute, App{Category: Coding})
	if len(evs) != 2 || evs[0].Kind != "morning" || evs[1].Kind != "long" || evs[1].Category != Coding {
		t.Fatalf("events = %+v, want morning then long coding", evs)
	}
	if evs[1].Minutes < 120 {
		t.Fatalf("long after %d minutes", evs[1].Minutes)
	}
}

func TestShortDetourKeepsTheStretch(t *testing.T) {
	var r Rules
	feed(&r, at(9, 0), time.Hour, App{Category: Coding})
	feed(&r, at(10, 0), 2*time.Minute, App{Category: Browsing}) // a quick look at the docs
	evs := feed(&r, at(10, 2), 70*time.Minute, App{Category: Coding})
	if len(evs) != 1 || evs[0].Kind != "long" {
		t.Fatalf("a 2-minute detour broke the stretch: %+v", evs)
	}
}

func TestLateNightGameIsNamed(t *testing.T) {
	var r Rules
	evs := feed(&r, at(2, 0), 10*time.Minute, App{Category: Gaming, Game: "ELDEN RING"})
	if len(evs) != 1 || evs[0].Kind != "late" || evs[0].Game != "ELDEN RING" {
		t.Fatalf("events = %+v", evs)
	}
	// once a night: still playing an hour later, nothing more about it
	for _, e := range feed(&r, at(2, 10), time.Hour, App{Category: Gaming, Game: "ELDEN RING"}) {
		if e.Kind == "late" {
			t.Fatalf("late again: %+v", e)
		}
	}
}

func TestQuietInMeetings(t *testing.T) {
	var r Rules
	if evs := feed(&r, at(9, 0), 3*time.Hour, App{Category: Meeting}); len(evs) != 0 {
		t.Fatalf("spoke in a meeting: %+v", evs)
	}
}

func TestBackAfterABreak(t *testing.T) {
	var r Rules
	feed(&r, at(13, 0), 30*time.Minute, App{Category: Writing})
	// away an hour: no samples
	evs := feed(&r, at(14, 30), time.Minute, App{Category: Writing})
	if len(evs) != 1 || evs[0].Kind != "back" || evs[0].Minutes < 45 {
		t.Fatalf("events = %+v", evs)
	}
}

func TestChatSwitching(t *testing.T) {
	var r Rules
	start := at(14, 0)
	var evs []Event
	for i := 0; i < 12; i++ { // back into the chat app every 4 minutes
		t0 := start.Add(time.Duration(i) * 4 * time.Minute)
		evs = append(evs, feed(&r, t0, 2*time.Minute, App{Category: Coding})...)
		evs = append(evs, feed(&r, t0.Add(2*time.Minute), 2*time.Minute, App{Category: Chatting})...)
	}
	found := false
	for _, e := range evs {
		if e.Kind == "switching" && e.Count >= switchCount {
			found = true
		}
	}
	if !found {
		t.Fatalf("no switching event: %+v", evs)
	}
}

func TestRateLimits(t *testing.T) {
	var r Rules
	// a back event every 50 minutes all day long: never more than 8, never closer than 20 min
	var evs []Event
	var times []time.Time
	for i := 0; i < 20; i++ {
		t0 := at(6, 0).Add(time.Duration(i) * 50 * time.Minute)
		for _, e := range feed(&r, t0, time.Minute, App{Category: Writing}) {
			evs = append(evs, e)
			times = append(times, t0)
		}
	}
	if len(evs) > maxPerDay {
		t.Fatalf("%d events in a day", len(evs))
	}
	for i := 1; i < len(times); i++ {
		if times[i].Sub(times[i-1]) < minGap {
			t.Fatalf("events %v apart", times[i].Sub(times[i-1]))
		}
	}
}

func TestSundayCard(t *testing.T) {
	var r Rules
	sunday := time.Date(2026, 10, 25, 19, 0, 0, 0, time.Local)
	card := func() Card { return Card{Days: 5, Grade: "B+"} }
	ev := r.Observe(sunday, App{Category: Coding}, card)
	for ev != nil && ev.Kind != "card" {
		ev = r.Observe(sunday.Add(25*time.Minute), App{Category: Coding}, card)
		sunday = sunday.Add(25 * time.Minute)
	}
	if ev == nil || ev.Card == nil || ev.Card.Grade != "B+" {
		t.Fatalf("no card on Sunday evening: %+v", ev)
	}
}

func TestQuietOverFullScreen(t *testing.T) {
	var r Rules
	feed(&r, at(13, 0), 10*time.Minute, App{Category: Gaming})
	if evs := feed(&r, at(13, 10), 3*time.Hour, App{Category: Gaming, Quiet: true}); len(evs) != 0 {
		t.Fatalf("spoke over a full-screen game: %+v", evs)
	}
	// out of full screen, still playing: now it may
	evs := feed(&r, at(16, 10), time.Minute, App{Category: Gaming})
	if len(evs) != 1 || evs[0].Kind != "long" {
		t.Fatalf("events = %+v", evs)
	}
}
