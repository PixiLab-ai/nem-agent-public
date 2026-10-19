package peek

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStatsKeepOnlyCategories(t *testing.T) {
	file := filepath.Join(t.TempDir(), "peek.json")
	s := LoadStats(file)
	now := at(10, 0)
	s.Add(now, Coding, 90*time.Minute)
	s.Add(now, Gaming, 30*time.Minute)
	s.Add(at(2, 0), Gaming, 20*time.Minute) // late
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got := LoadStats(file)
	if m := got.Today(now); m[Coding] != 90 || m[Gaming] != 50 {
		t.Fatalf("today = %v", m)
	}
	if d := got.Days[now.Format(dayLayout)]; d.Late != 20 {
		t.Fatalf("late = %v", d.Late)
	}
	if err := got.Forget(); err != nil {
		t.Fatal(err)
	}
	if again := LoadStats(file); len(again.Days) != 0 {
		t.Fatal("forget kept something")
	}
}

func TestCardGrades(t *testing.T) {
	s := LoadStats("")
	base := at(12, 0)
	for d := 0; d < 5; d++ {
		day := base.AddDate(0, 0, -d)
		s.Add(day, Coding, 4*time.Hour)
		s.Add(day, Gaming, time.Hour)
	}
	c := s.Card(base)
	if c.Days != 5 || len(c.Top) != 2 || c.Top[0].Category != Coding || c.Grade != "A+" {
		t.Fatalf("card = %+v", c)
	}
	s.Add(base, Gaming, 60*time.Hour) // a gaming weekend (and then some)
	for d := 0; d < 4; d++ {
		s.Add(time.Date(base.Year(), base.Month(), base.Day()-d, 2, 0, 0, 0, time.Local), Gaming, 30*time.Minute)
	}
	if c := s.Card(base); c.Grade != "D−" || c.Late != 4 {
		t.Fatalf("card = %+v", c)
	}
	if c := LoadStats("").Card(base); c.Grade != "" {
		t.Fatalf("an empty week has a grade: %+v", c)
	}
}

// What was said once today stays said after a restart (an update in the morning: no second hello).
func TestSaidSurvivesARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "peek.json")
	p := New(Options{StatsFile: file, Now: func() time.Time { return at(9, 0) }})
	p.stats.noteSaid(at(9, 0), "morning")
	p.stats.noteSaid(at(9, 0), "long") // not a once-a-day thing
	if err := p.stats.Save(); err != nil {
		t.Fatal(err)
	}
	again := New(Options{StatsFile: file})
	if ev := again.rules.Observe(at(9, 30), App{Category: Coding}, nil); ev != nil {
		t.Fatalf("said again after a restart: %+v", ev)
	}
	if len(again.stats.Said) != 1 {
		t.Fatalf("said = %v", again.stats.Said)
	}
	if err := again.Forget(); err != nil || again.stats.Said != nil {
		t.Fatalf("forget kept %v (%v)", again.stats.Said, err)
	}
}
