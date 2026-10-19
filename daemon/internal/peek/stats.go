package peek

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Stats is everything Peek keeps, on this computer only: minutes per category per day for 7 days,
// and the minutes past 1 am. No app, no title, no game's name.
type Stats struct {
	Version int             `json:"version"`
	Days    map[string]*Day `json:"days"` // "2006-01-02", local time
	// Said: the once-a-day (or week) things the pet already said, so a restart doesn't repeat
	// them — kind → the day (morning, late) or the week (card)
	Said  map[string]string `json:"said,omitempty"`
	file  string
	dirty bool
}

// Day is one day's minutes.
type Day struct {
	Minutes map[Category]float64 `json:"minutes"`
	Late    float64              `json:"late"` // minutes between 01:00 and 05:00
}

const (
	statsVersion = 1
	keepDays     = 7
	dayLayout    = "2006-01-02"
)

// LoadStats reads the stats file (a missing or unreadable one starts empty).
func LoadStats(file string) *Stats {
	s := &Stats{Version: statsVersion, Days: map[string]*Day{}, file: file}
	raw, err := os.ReadFile(file)
	if err != nil {
		return s
	}
	var got Stats
	if json.Unmarshal(raw, &got) == nil && got.Version == statsVersion && got.Days != nil {
		s.Days, s.Said = got.Days, got.Said
	}
	return s
}

// noteSaid remembers a once-a-day (or week) thing the pet said.
func (s *Stats) noteSaid(now time.Time, kind string) {
	key := now.Format(dayLayout)
	switch kind {
	case "card":
		key = isoWeek(now)
	case "morning", "late":
	default:
		return
	}
	if s.Said == nil {
		s.Said = map[string]string{}
	}
	s.Said[kind] = key
	s.dirty = true
}

// Add counts d of category c at now.
func (s *Stats) Add(now time.Time, c Category, d time.Duration) {
	if c == None || d <= 0 {
		return
	}
	key := now.Format(dayLayout)
	day := s.Days[key]
	if day == nil {
		day = &Day{Minutes: map[Category]float64{}}
		s.Days[key] = day
		s.prune(now)
	}
	day.Minutes[c] += d.Minutes()
	if h := now.Hour(); h >= 1 && h < 5 {
		day.Late += d.Minutes()
	}
	s.dirty = true
}

func (s *Stats) prune(now time.Time) {
	cut := now.AddDate(0, 0, -keepDays).Format(dayLayout)
	for k := range s.Days {
		if k <= cut {
			delete(s.Days, k)
		}
	}
}

// Save writes the stats when they changed (atomically, this user only).
func (s *Stats) Save() error {
	if !s.dirty || s.file == "" {
		return nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.file); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// Forget wipes every stat, on disk too.
func (s *Stats) Forget() error {
	s.Days, s.Said = map[string]*Day{}, nil
	s.dirty = false
	if s.file == "" {
		return nil
	}
	if err := os.Remove(s.file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Today is today's whole minutes per category.
func (s *Stats) Today(now time.Time) map[Category]int {
	out := map[Category]int{}
	if day := s.Days[now.Format(dayLayout)]; day != nil {
		for c, m := range day.Minutes {
			if n := int(m); n > 0 {
				out[c] = n
			}
		}
	}
	return out
}

// LateToday is today's whole minutes between 1 and 5 am.
func (s *Stats) LateToday(now time.Time) int {
	if day := s.Days[now.Format(dayLayout)]; day != nil {
		return int(day.Late)
	}
	return 0
}

// CatMinutes is one line of the report card.
type CatMinutes struct {
	Category Category `json:"category"`
	Minutes  int      `json:"minutes"`
}

// Card is the week's report card: the biggest categories, the late nights, a grade.
type Card struct {
	Top   []CatMinutes `json:"top"`
	Late  int          `json:"late"`  // nights with 15 minutes or more past 1 am
	Days  int          `json:"days"`  // days with anything
	Grade string       `json:"grade"` // A+ … D−; "" = not enough to judge
}

var focusCats = map[Category]bool{Coding: true, Terminal: true, Writing: true, Creating: true}
var funCats = map[Category]bool{Gaming: true, Video: true, Chatting: true, Browsing: true}

// Card is the last 7 days' report card.
func (s *Stats) Card(now time.Time) Card {
	totals := map[Category]float64{}
	var c Card
	cut := now.AddDate(0, 0, -keepDays).Format(dayLayout)
	for k, day := range s.Days {
		if k <= cut {
			continue
		}
		any := false
		for cat, m := range day.Minutes {
			totals[cat] += m
			any = any || m >= 1
		}
		if any {
			c.Days++
		}
		if day.Late >= 15 {
			c.Late++
		}
	}
	for cat, m := range totals {
		if cat != Other && cat != None && m >= 1 {
			c.Top = append(c.Top, CatMinutes{Category: cat, Minutes: int(m)})
		}
	}
	sort.Slice(c.Top, func(i, j int) bool {
		if c.Top[i].Minutes != c.Top[j].Minutes {
			return c.Top[i].Minutes > c.Top[j].Minutes
		}
		return c.Top[i].Category < c.Top[j].Category
	})
	if len(c.Top) > 3 {
		c.Top = c.Top[:3]
	}
	var focus, fun float64
	for cat, m := range totals {
		if focusCats[cat] {
			focus += m
		}
		if funCats[cat] {
			fun += m
		}
	}
	if focus+fun >= 120 { // two hours at least, or there's nothing to judge
		r := focus / (focus + fun)
		switch {
		case r >= 0.7:
			c.Grade = "A"
		case r >= 0.5:
			c.Grade = "B"
		case r >= 0.3:
			c.Grade = "C"
		default:
			c.Grade = "D"
		}
		switch {
		case c.Late == 0:
			c.Grade += "+"
		case c.Late >= 4:
			c.Grade += "−"
		}
	}
	return c
}
