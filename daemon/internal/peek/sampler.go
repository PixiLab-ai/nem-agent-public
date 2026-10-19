package peek

// Modes: how much this computer says about what's in front.
const (
	ModeFront   = "front"   // which app is in front (X11, Windows, macOS)
	ModeRunning = "running" // only what's running: games and meeting apps (a Wayland session)
	ModeNone    = "none"    // nothing (an OS Peek doesn't know)
)

// Sampler reads the app in front — never its title, nothing on the screen.
type Sampler interface {
	// Front is the app in front now; Front{} when there's none (the desktop) or it can't be told.
	Front() (Front, error)
	Close()
}
