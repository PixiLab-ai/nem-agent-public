package peek

import (
	"fmt"
	"time"
)

// Event is something the pet may say something about. Game is a game's name (the pet's bubble
// only: never stored, never sent).
type Event struct {
	Kind     string   `json:"kind"` // morning · back · late · long · switching · card
	Category Category `json:"category,omitempty"`
	Minutes  int      `json:"minutes,omitempty"`
	Count    int      `json:"count,omitempty"`
	Game     string   `json:"game,omitempty"`
	Card     *Card    `json:"card,omitempty"`
}

const (
	longAfter    = 2 * time.Hour    // one thing this long: "two hours of code"
	streakGrace  = 3 * time.Minute  // a short detour doesn't end it
	gapBreak     = 10 * time.Minute // away this long (no samples) ends it
	backAfter    = 45 * time.Minute // away this long: "you're back"
	minGap       = 20 * time.Minute // between two things the pet says
	maxPerDay    = 8
	switchCount  = 10 // back into a chat app this many times in an hour
	switchWindow = time.Hour
	switchEvery  = 2 * time.Hour
)

// what a long stretch of is worth a word, and what is at 2 am
var longCats = map[Category]bool{Coding: true, Terminal: true, Writing: true, Creating: true, Gaming: true, Video: true, Browsing: true}
var lateCats = map[Category]bool{Coding: true, Terminal: true, Gaming: true, Video: true, Browsing: true, Chatting: true, Writing: true, Creating: true}

// Rules decides when the pet says something: rarely (20 minutes apart, 8 a day), never in a
// meeting, never the same thing twice in a row.
type Rules struct {
	streakCat      Category
	streakSince    time.Time
	otherSince     time.Time
	streakReported bool
	streakGame     string

	lastSample time.Time
	lastActive time.Time
	lastCat    Category
	chats      []time.Time

	dayKey      string
	eventsToday int
	lastEvent   time.Time
	lastKind    string
	morningDay  string
	lateDay     string
	lastSwitch  time.Time
	cardWeek    string
}

// Observe takes one sample (what's in front at now) and answers what the pet says, if anything.
// card gives the week's report card (Sunday evening).
func (r *Rules) Observe(now time.Time, app App, card func() Card) *Event {
	day := now.Format(dayLayout)
	if day != r.dayKey {
		r.dayKey, r.eventsToday = day, 0
	}
	gap := r.lastSample.IsZero() || now.Sub(r.lastSample) > gapBreak
	r.lastSample = now
	if app.Category == None {
		return nil
	}
	away := time.Duration(0)
	if !r.lastActive.IsZero() {
		away = now.Sub(r.lastActive)
	}
	firstToday := r.lastActive.IsZero() || r.lastActive.Format(dayLayout) != day
	r.lastActive = now

	// the stretch of one thing
	switch {
	case gap || r.streakCat == None:
		r.startStreak(app, now)
	case app.Category == r.streakCat:
		r.otherSince = time.Time{}
		if app.Game != "" {
			r.streakGame = app.Game
		}
	case r.otherSince.IsZero():
		r.otherSince = now
	case now.Sub(r.otherSince) >= streakGrace:
		r.startStreak(app, r.otherSince)
	}
	// back into a chat app
	if app.Category == Chatting && r.lastCat != Chatting {
		r.chats = append(r.chats, now)
	}
	for len(r.chats) > 0 && now.Sub(r.chats[0]) > switchWindow {
		r.chats = r.chats[1:]
	}
	r.lastCat = app.Category
	if app.Category == Meeting || app.Quiet {
		return nil // quiet in a meeting, over something full screen
	}

	var cands []Event
	if firstToday && now.Hour() >= 5 && now.Hour() < 12 && r.morningDay != day {
		cands = append(cands, Event{Kind: "morning", Category: app.Category})
	}
	if !firstToday && away >= backAfter {
		cands = append(cands, Event{Kind: "back", Category: app.Category, Minutes: int(away.Minutes())})
	}
	if h := now.Hour(); h >= 1 && h < 5 && r.lateDay != day && lateCats[app.Category] {
		cands = append(cands, Event{Kind: "late", Category: app.Category, Game: app.Game})
	}
	if !r.streakReported && app.Category == r.streakCat && longCats[app.Category] && now.Sub(r.streakSince) >= longAfter {
		cands = append(cands, Event{Kind: "long", Category: app.Category, Minutes: int(now.Sub(r.streakSince).Minutes()), Game: r.streakGame})
	}
	if len(r.chats) >= switchCount && now.Sub(r.lastSwitch) >= switchEvery {
		cands = append(cands, Event{Kind: "switching", Category: Chatting, Count: len(r.chats)})
	}
	if now.Weekday() == time.Sunday && now.Hour() >= 18 && r.cardWeek != isoWeek(now) && card != nil {
		if c := card(); c.Days >= 3 {
			cands = append(cands, Event{Kind: "card", Card: &c})
		}
	}
	for _, ev := range cands {
		if !r.allowed(now, ev.Kind) {
			continue
		}
		r.lastEvent, r.lastKind = now, ev.Kind
		r.eventsToday++
		switch ev.Kind {
		case "morning":
			r.morningDay = day
		case "late":
			r.lateDay = day
		case "long":
			r.streakReported = true
		case "switching":
			r.lastSwitch, r.chats = now, nil
		case "card":
			r.cardWeek = isoWeek(now)
		}
		e := ev
		return &e
	}
	return nil
}

// Stretch is what's been in front without a break, and for how long (None: nothing lately).
func (r *Rules) Stretch(now time.Time) (Category, int) {
	if r.streakCat == None || r.lastSample.IsZero() || now.Sub(r.lastSample) > gapBreak {
		return None, 0
	}
	return r.streakCat, int(now.Sub(r.streakSince).Minutes())
}

// restore picks up what was already said today (or this week) before a restart.
func (r *Rules) restore(said map[string]string) {
	r.morningDay, r.lateDay, r.cardWeek = said["morning"], said["late"], said["card"]
}

func (r *Rules) startStreak(app App, since time.Time) {
	r.streakCat, r.streakSince, r.otherSince, r.streakReported, r.streakGame = app.Category, since, time.Time{}, false, app.Game
}

func (r *Rules) allowed(now time.Time, kind string) bool {
	if r.eventsToday >= maxPerDay {
		return false
	}
	if !r.lastEvent.IsZero() && now.Sub(r.lastEvent) < minGap {
		return false
	}
	return kind != r.lastKind || kind == "back"
}

func isoWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-%02d", y, w)
}
