package peek

// What her pet hears (the site's POST …/pet/peek): kinds of things and minutes. Never an app, a
// title, a file — nor a game's name, which stays in the app's own lines on this computer.

// Report is how things stand (or Off: the pet forgets it now).
type Report struct {
	Off   bool             `json:"off,omitempty"`
	Now   *Stretch         `json:"now,omitempty"`
	Today map[Category]int `json:"today,omitempty"`
	Late  int              `json:"late,omitempty"` // minutes between 1 and 5 am today
	Event *ReportEvent     `json:"event,omitempty"`
}

// Stretch is what's in front without a break, and for how long. Quiet: full screen or a meeting
// (the pet keeps its idle lines for later).
type Stretch struct {
	Category Category `json:"category"`
	Minutes  int      `json:"minutes"`
	Quiet    bool     `json:"quiet,omitempty"`
}

// ReportEvent is a moment worth a word, without the game's name.
type ReportEvent struct {
	Kind     string   `json:"kind"`
	Category Category `json:"category,omitempty"`
	Minutes  int      `json:"minutes,omitempty"`
	Count    int      `json:"count,omitempty"`
	Card     *Card    `json:"card,omitempty"`
	Asked    bool     `json:"asked,omitempty"` // "Judge me"
}

func reportEvent(ev Event) *ReportEvent {
	return &ReportEvent{Kind: ev.Kind, Category: ev.Category, Minutes: ev.Minutes, Count: ev.Count, Card: ev.Card}
}
