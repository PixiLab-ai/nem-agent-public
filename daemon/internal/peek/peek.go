package peek

import (
	"context"
	"sync"
	"time"
)

// reportEvery: while Peek looks, her pet hears how things stand this often — and sooner when the
// stretch changes or goes full screen, and with every moment worth a word.
const reportEvery = 5 * time.Minute

// Options set Peek up.
type Options struct {
	StatsFile string
	// Report sends what her pet may hear to the site (the daemon queues it, in order); ev is the
	// moment it carries, for the app's own line when the site can't answer. nil: the app's own
	// lines only (Emit).
	Report     func(r Report, ev *Event)
	Emit       func(Event) // the app's own line, in the pet's bubble
	Active     func() bool // the human is at the computer (nemd's presence)
	Every      time.Duration
	NewSampler func() (Sampler, string) // tests; default NewSampler
	Now        func() time.Time
}

// Status is what the settings show.
type Status struct {
	On          bool       `json:"on"`
	PausedUntil *time.Time `json:"paused_until,omitempty"`
	Mode        string     `json:"mode,omitempty"` // front · running · none (once it has run)
}

// Peek runs the sampling while it's on, not paused, and the human is at the computer: what's in
// front → a category → today's minutes → what her pet hears, and maybe a word from it.
type Peek struct {
	o       Options
	mu      sync.Mutex
	on      bool
	paused  time.Time
	sampler Sampler
	mode    string
	stats   *Stats
	rules   Rules
	last    time.Time
	wake    chan struct{}

	lastReport    time.Time
	reportedCat   Category
	reportedQuiet bool
}

func New(o Options) *Peek {
	if o.Every <= 0 {
		o.Every = 20 * time.Second
	}
	if o.NewSampler == nil {
		o.NewSampler = NewSampler
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Emit == nil {
		o.Emit = func(Event) {}
	}
	if o.Active == nil {
		o.Active = func() bool { return true }
	}
	p := &Peek{o: o, stats: LoadStats(o.StatsFile), wake: make(chan struct{}, 1)}
	p.rules.restore(p.stats.Said)
	return p
}

// SetOn turns Peek on or off (off: the sampler lets go of the display at once, and her pet
// forgets what it heard).
func (p *Peek) SetOn(on bool) {
	p.mu.Lock()
	was := p.on
	p.on = on
	if !on {
		p.paused = time.Time{}
		p.closeSampler()
	}
	p.mu.Unlock()
	if was && !on {
		p.tellOff()
	}
	p.poke()
}

// Pause stops looking for d (0 resumes); her pet forgets what it heard meanwhile.
func (p *Peek) Pause(d time.Duration) {
	p.mu.Lock()
	on := p.on
	if d > 0 {
		p.paused = p.o.Now().Add(d)
	} else {
		p.paused = time.Time{}
	}
	p.mu.Unlock()
	if on && d > 0 {
		p.tellOff()
	}
	p.poke()
}

func (p *Peek) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Status{On: p.on, Mode: p.mode}
	if p.paused.After(p.o.Now()) {
		until := p.paused
		st.PausedUntil = &until
	}
	return st
}

// Today is today's minutes per category.
func (p *Peek) Today() map[Category]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats.Today(p.o.Now())
}

// Card is the week's report card.
func (p *Peek) Card() Card {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats.Card(p.o.Now())
}

// Judge is "Judge me": the week's card as a report her pet answers (asked: past its budget), and
// the moment itself for the app's own line. on=false: Peek is off, nothing to send.
func (p *Peek) Judge() (r Report, ev Event, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c := p.stats.Card(p.o.Now())
	ev = Event{Kind: "card", Card: &c}
	re := reportEvent(ev)
	re.Asked = true
	return Report{Event: re}, ev, p.on
}

// Forget wipes the stats; her pet forgets too.
func (p *Peek) Forget() error {
	p.mu.Lock()
	p.rules = Rules{}
	err := p.stats.Forget()
	on := p.on
	p.mu.Unlock()
	if on {
		p.tellOff()
	}
	return err
}

// tellOff: her pet forgets what it heard (Peek off, paused, forgotten); the next look tells it
// afresh.
func (p *Peek) tellOff() {
	p.mu.Lock()
	p.lastReport, p.reportedCat, p.reportedQuiet = time.Time{}, None, false
	p.mu.Unlock()
	if p.o.Report != nil {
		p.o.Report(Report{Off: true}, nil)
	}
}

func (p *Peek) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *Peek) closeSampler() {
	if p.sampler != nil {
		p.sampler.Close()
		p.sampler = nil
	}
}

// Run samples until ctx ends.
func (p *Peek) Run(ctx context.Context) {
	tick := time.NewTicker(p.o.Every)
	defer tick.Stop()
	saveEvery := 0
	for {
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.closeSampler()
			_ = p.stats.Save()
			p.mu.Unlock()
			return
		case <-tick.C:
		case <-p.wake:
			continue
		}
		ev, r := p.sample()
		switch {
		case r != nil && p.o.Report != nil:
			p.o.Report(*r, ev)
		case ev != nil:
			p.o.Emit(*ev)
		}
		if saveEvery++; saveEvery >= 9 { // every few minutes
			saveEvery = 0
			p.mu.Lock()
			_ = p.stats.Save()
			p.mu.Unlock()
		}
	}
}

// sample is one look (when allowed): what's in front, counted, judged — and, when it's time, what
// her pet hears.
func (p *Peek) sample() (*Event, *Report) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.o.Now()
	if !p.on || p.paused.After(now) || !p.o.Active() {
		p.last = time.Time{}
		return nil, nil
	}
	if p.sampler == nil {
		p.sampler, p.mode = p.o.NewSampler()
	}
	f, err := p.sampler.Front()
	if err != nil {
		return nil, nil
	}
	app := Categorize(f)
	app.Quiet = f.Quiet
	d := p.o.Every
	if !p.last.IsZero() && now.Sub(p.last) < d {
		d = now.Sub(p.last)
	}
	p.last = now
	p.stats.Add(now, app.Category, d)
	ev := p.rules.Observe(now, app, func() Card { return p.stats.Card(now) })
	if ev != nil {
		p.stats.noteSaid(now, ev.Kind)
	}
	cat, mins := p.rules.Stretch(now)
	quiet := app.Quiet || app.Category == Meeting
	changed := (cat != p.reportedCat && mins >= 2) || quiet != p.reportedQuiet
	if ev == nil && !changed && now.Sub(p.lastReport) < reportEvery {
		return nil, nil
	}
	p.lastReport, p.reportedCat, p.reportedQuiet = now, cat, quiet
	r := Report{Today: p.stats.Today(now), Late: p.stats.LateToday(now)}
	if cat != None {
		r.Now = &Stretch{Category: cat, Minutes: mins, Quiet: quiet}
	}
	if ev != nil {
		r.Event = reportEvent(*ev)
	}
	return ev, &r
}
