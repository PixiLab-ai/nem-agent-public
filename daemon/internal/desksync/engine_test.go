package desksync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rig struct {
	t     *testing.T
	desk  *fakeDesk
	root  string
	trash string
	eng   *Engine
	ctx   context.Context
}

func newRig(t *testing.T) *rig {
	t.Helper()
	desk := newFakeDesk()
	root := t.TempDir()
	if err := Claim(root, "c-1", "https://nemini.test"); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(filepath.Join(t.TempDir(), "desk-c-1.json"), "c-1", root)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, desk: desk, root: root, trash: t.TempDir(), ctx: context.Background()}
	r.eng = New(Options{
		Root: root, Companion: "c-1", Device: "test box", Remote: desk, Fetch: desk.fetch, State: st,
		Trash: r.trash, Rate: 1000, Burst: 1000,
	})
	return r
}

func (r *rig) pass() {
	r.t.Helper()
	if err := r.eng.pass(r.ctx); err != nil {
		r.t.Fatalf("pass: %v", err)
	}
}

func (r *rig) local(p string) string {
	return filepath.Join(r.root, filepath.FromSlash(strings.TrimPrefix(p, "/")))
}

func (r *rig) read(p string) (string, bool) {
	b, err := os.ReadFile(r.local(p))
	return string(b), err == nil
}

// write puts a file in the folder as if the human saved it a minute ago (past the quiet window)
func (r *rig) write(p, body string) {
	r.t.Helper()
	l := r.local(p)
	if err := os.MkdirAll(filepath.Dir(l), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(l, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(l, old, old); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) called(prefix string) bool {
	for _, c := range r.desk.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func (r *rig) conflictCopies(dir string) []string {
	var out []string
	entries, _ := os.ReadDir(r.local(dir))
	for _, e := range entries {
		if strings.Contains(e.Name(), "(conflict from test box ") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestColdStartDownloadsEverything(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/notes/a.md", "hello")
	r.desk.herBlob("/images/p.png", []byte{0x89, 'P', 'N', 'G', 0, 1, 2})
	r.desk.herText("/personality.md", "# me")
	r.desk.herText("/gone.md", "x")
	r.desk.herDelete("/gone.md")
	r.pass()
	if s, ok := r.read("/notes/a.md"); !ok || s != "hello" {
		t.Fatalf("text: %q %v", s, ok)
	}
	if s, ok := r.read("/images/p.png"); !ok || s != "\x89PNG\x00\x01\x02" {
		t.Fatalf("blob: %q %v", s, ok)
	}
	if _, ok := r.read("/personality.md"); !ok {
		t.Fatal("her personality didn't come down")
	}
	if _, ok := r.read("/gone.md"); ok {
		t.Fatal("a deleted file came down")
	}
	if !r.eng.s.Ready || r.eng.s.Cursor != r.desk.seq {
		t.Fatalf("state: ready %v cursor %d head %d", r.eng.s.Ready, r.eng.s.Cursor, r.desk.seq)
	}
	if len(r.desk.calls) != 0 {
		t.Fatalf("the first download wrote to her desk: %v", r.desk.calls)
	}
	if _, err := os.Stat(filepath.Join(r.root, MarkerName)); err != nil {
		t.Fatal("the marker is gone")
	}
}

func TestHerChangesComeDown(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/a.md", "one")
	r.desk.herText("/b.md", "bee")
	r.desk.herText("/c.md", "sea")
	r.pass()
	r.desk.herText("/a.md", "two")          // an edit
	r.desk.herText("/new.md", "fresh")      // a new file
	r.desk.herDelete("/b.md")               // a delete
	r.desk.herMove("/c.md", "/moved/c2.md") // a move
	r.pass()
	if s, _ := r.read("/a.md"); s != "two" {
		t.Fatalf("her edit: %q", s)
	}
	if s, _ := r.read("/new.md"); s != "fresh" {
		t.Fatalf("her new file: %q", s)
	}
	if _, ok := r.read("/b.md"); ok {
		t.Fatal("her delete left the file")
	}
	trashed, _ := filepath.Glob(filepath.Join(r.trash, "*", "b.md"))
	if len(trashed) != 1 {
		t.Fatalf("her delete isn't in the local trash: %v", trashed)
	}
	if _, ok := r.read("/c.md"); ok {
		t.Fatal("her move left the old file")
	}
	if s, _ := r.read("/moved/c2.md"); s != "sea" {
		t.Fatalf("her move: %q", s)
	}
	if len(r.desk.calls) != 0 {
		t.Fatalf("her changes echoed back to her desk: %v", r.desk.calls)
	}
}

func TestFolderChangesGoUp(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/a.md", "one")
	r.desk.herText("/b.md", "bee")
	r.pass()
	r.write("/a.md", "edited here")
	r.write("/docs/c.md", "new here")
	if err := os.Remove(r.local("/b.md")); err != nil {
		t.Fatal(err)
	}
	r.pass()
	if row := r.desk.live("/a.md"); row == nil || row.text != "edited here" || row.f.Origin != "sync" {
		t.Fatalf("the edit didn't go up: %+v", row)
	}
	if row := r.desk.live("/docs/c.md"); row == nil || row.text != "new here" {
		t.Fatalf("the new file didn't go up: %+v", row)
	}
	if r.desk.live("/b.md") != nil {
		t.Fatal("the delete didn't go up")
	}
	// a second pass sends nothing more: everything agrees
	n := len(r.desk.calls)
	r.pass()
	if len(r.desk.calls) != n {
		t.Fatalf("an idle pass wrote again: %v", r.desk.calls[n:])
	}
}

// Her system files come down, but a change made to them here is undone.
func TestProtectedFilesAreDownloadOnly(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/personality.md", "# her")
	r.desk.herBlob("/avatar.png", []byte("face"))
	r.pass()
	r.write("/personality.md", "# mine now")
	r.write("/avatar.png", "another face")
	r.pass()
	if row := r.desk.live("/personality.md"); row.text != "# her" {
		t.Fatalf("her personality was overwritten: %q", row.text)
	}
	if s, _ := r.read("/personality.md"); s != "# her" {
		t.Fatalf("her version didn't come back: %q", s)
	}
	if s, _ := r.read("/avatar.png"); s != "face" {
		t.Fatalf("her avatar didn't come back: %q", s)
	}
	if r.called("put /personality.md") || r.called("put /avatar.png") {
		t.Fatalf("a protected file was sent: %v", r.desk.calls)
	}
	// the local edit is kept in the trash, not beside her file (a copy there would go up as new)
	if c := r.conflictCopies("/"); len(c) != 0 {
		t.Fatalf("conflict copies of her files in the folder: %v", c)
	}
	if kept, _ := filepath.Glob(filepath.Join(r.trash, "*", "personality.md")); len(kept) != 1 {
		t.Fatalf("the local edit isn't in the trash: %v", kept)
	}
	r.pass()
	for _, c := range r.desk.calls {
		if strings.HasPrefix(c, "put ") {
			t.Fatalf("something went up after the restore: %v", r.desk.calls)
		}
	}
	// deleting one here brings it back too
	if err := os.Remove(r.local("/personality.md")); err != nil {
		t.Fatal(err)
	}
	r.pass()
	if s, ok := r.read("/personality.md"); !ok || s != "# her" {
		t.Fatalf("a deleted protected file didn't come back: %q %v", s, ok)
	}
	if r.called("del /personality.md") {
		t.Fatal("a protected delete was sent")
	}
}

func TestBothChangedKeepsBoth(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/a.md", "base")
	r.pass()
	r.desk.herText("/a.md", "hers")
	r.write("/a.md", "mine")
	r.pass()
	if s, _ := r.read("/a.md"); s != "hers" {
		t.Fatalf("her version should keep the path: %q", s)
	}
	copies := r.conflictCopies("/")
	if len(copies) != 1 {
		t.Fatalf("conflict copies: %v", copies)
	}
	if s, _ := r.read("/" + copies[0]); s != "mine" {
		t.Fatalf("the conflict copy: %q", s)
	}
	// the copy goes up as a new file on a later pass (once quiet)
	old := time.Now().Add(-time.Minute)
	_ = os.Chtimes(r.local("/"+copies[0]), old, old)
	r.pass()
	if r.desk.live("/"+copies[0]) == nil {
		t.Fatal("the conflict copy didn't go up")
	}
	if row := r.desk.live("/a.md"); row.text != "hers" {
		t.Fatalf("her version was overwritten: %q", row.text)
	}
}

func TestEditBeatsHerDelete(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/a.md", "base")
	r.pass()
	r.desk.herDelete("/a.md")
	r.write("/a.md", "kept and edited")
	r.pass()
	if s, ok := r.read("/a.md"); !ok || s != "kept and edited" {
		t.Fatalf("the local edit was lost: %q %v", s, ok)
	}
	if row := r.desk.live("/a.md"); row == nil || row.text != "kept and edited" {
		t.Fatalf("the edit didn't go back up: %+v", row)
	}
}

func TestLocalRenameIsAMove(t *testing.T) {
	r := newRig(t)
	r.desk.herBlob("/images/p.png", []byte("pixels"))
	r.pass()
	uuid := r.desk.live("/images/p.png").f.UUID
	if err := os.MkdirAll(r.local("/album"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(r.local("/images/p.png"), r.local("/album/p.png")); err != nil {
		t.Fatal(err)
	}
	r.pass()
	row := r.desk.live("/album/p.png")
	if row == nil || row.f.UUID != uuid {
		t.Fatalf("the rename wasn't a move (same uuid): %+v calls %v", row, r.desk.calls)
	}
	if r.desk.live("/images/p.png") != nil || r.called("put /album/p.png") {
		t.Fatalf("the rename went up as a delete + an upload: %v", r.desk.calls)
	}
}

func TestIgnoredFilesStayHere(t *testing.T) {
	r := newRig(t)
	r.pass()
	for _, p := range []string{"/.DS_Store", "/notes.md.swp", "/.env", "/x/~$report.docx", "/id_rsa", "/download.crdownload", "/.hidden-new.md"} {
		r.write(p, "x")
	}
	r.pass()
	for _, c := range r.desk.calls {
		if strings.HasPrefix(c, "put ") {
			t.Fatalf("an ignored file was sent: %v", r.desk.calls)
		}
	}
}

func TestLockedFileRefusesTheDelete(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/keep.md", "mine forever")
	r.desk.lock("/keep.md")
	r.pass()
	if err := os.Remove(r.local("/keep.md")); err != nil {
		t.Fatal(err)
	}
	r.pass()
	if r.desk.live("/keep.md") == nil {
		t.Fatal("a locked file was deleted")
	}
	if r.called("del /keep.md") {
		t.Fatal("a delete was sent for a locked file (an older server would let it through)")
	}
	if s, ok := r.read("/keep.md"); !ok || s != "mine forever" {
		t.Fatalf("the locked file didn't come back: %q %v", s, ok)
	}
}

func TestMissingFolderDeletesNothing(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/a.md", "x")
	r.pass()
	if err := os.Remove(filepath.Join(r.root, MarkerName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.local("/a.md")); err != nil {
		t.Fatal(err)
	}
	if err := r.eng.pass(r.ctx); err != errPaused {
		t.Fatalf("pass without the marker = %v, want paused", err)
	}
	if r.desk.live("/a.md") == nil || r.called("del") {
		t.Fatalf("something was deleted: %v", r.desk.calls)
	}
}

func TestMassDeleteWaitsForAnAnswer(t *testing.T) {
	r := newRig(t)
	for i := 0; i < 150; i++ {
		r.desk.herText("/many/"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".md", "x")
	}
	r.pass()
	if err := os.RemoveAll(r.local("/many")); err != nil {
		t.Fatal(err)
	}
	r.pass()
	if st := r.eng.Status(); st.State != "waiting" || st.Detail != "mass_delete" || st.Count != 150 {
		t.Fatalf("status = %+v, want waiting for 150", st)
	}
	if r.called("del") {
		t.Fatal("deleted before the answer")
	}
	// no: they come back
	r.eng.Answer(false)
	r.pass()
	if _, ok := r.read("/many/a.md"); !ok {
		t.Fatal("the files didn't come back")
	}
	if r.called("del") {
		t.Fatal("deleted after a no")
	}
	// again, and yes this time
	if err := os.RemoveAll(r.local("/many")); err != nil {
		t.Fatal(err)
	}
	r.pass()
	r.eng.Answer(true)
	r.pass()
	if r.desk.live("/many/a.md") != nil {
		t.Fatal("not deleted after a yes")
	}
}

func TestFailedDownloadIsNotADelete(t *testing.T) {
	r := newRig(t)
	r.desk.herBlob("/big.bin", []byte("bytes"))
	r.desk.failDL[r.desk.live("/big.bin").f.URL] = true
	r.pass()
	if _, ok := r.read("/big.bin"); ok {
		t.Fatal("a failed download left a file")
	}
	r.pass()
	if r.desk.live("/big.bin") == nil || r.called("del") {
		t.Fatalf("a file that never came down was deleted: %v", r.desk.calls)
	}
}

func TestHerFolderMoveIsOneRename(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/proj/a.md", "a")
	r.desk.herText("/proj/sub/b.md", "b")
	r.pass()
	// her desktop_move of a folder: every row under it changes path, the uuids stay
	r.desk.mu.Lock()
	for _, p := range []string{"/proj/sub/b.md", "/proj/a.md", "/proj/sub", "/proj"} {
		r.desk.mv(p, "/archive"+strings.TrimPrefix(p, "/proj"))
	}
	r.desk.mu.Unlock()
	r.pass()
	if s, _ := r.read("/archive/sub/b.md"); s != "b" {
		t.Fatalf("her folder move: %q", s)
	}
	if _, err := os.Stat(r.local("/proj")); err == nil {
		t.Fatal("the old folder is still here")
	}
	if len(r.desk.calls) != 0 {
		t.Fatalf("her move echoed back: %v", r.desk.calls)
	}
}

func TestRelinkAdoptsTheSameFiles(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/a.md", "same")
	r.desk.herText("/b.md", "hers")
	r.write("/a.md", "same")      // already here, the same bytes (a reinstall)
	r.write("/b.md", "different") // already here, other bytes
	r.pass()
	if s, _ := r.read("/a.md"); s != "same" {
		t.Fatalf("a.md: %q", s)
	}
	if s, _ := r.read("/b.md"); s != "hers" {
		t.Fatalf("b.md should be hers: %q", s)
	}
	if c := r.conflictCopies("/"); len(c) != 1 {
		t.Fatalf("the different local b.md should be kept beside: %v", c)
	}
	if r.called("put /a.md") {
		t.Fatal("an identical file was sent again")
	}
}

func TestFailedDownloadIsRetried(t *testing.T) {
	r := newRig(t)
	r.desk.herBlob("/flaky.png", []byte("pixels"))
	url := r.desk.live("/flaky.png").f.URL
	r.desk.failDL[url] = true
	r.pass()
	if _, ok := r.read("/flaky.png"); ok {
		t.Fatal("came down while failing")
	}
	delete(r.desk.failDL, url) // the CDN is fine again
	r.pass()
	if s, ok := r.read("/flaky.png"); !ok || s != "pixels" {
		t.Fatalf("not retried: %q %v", s, ok)
	}
	if en := r.eng.s.Entries["/flaky.png"]; en == nil || en.Missing != "" {
		t.Fatalf("still marked missing: %+v", en)
	}
}

// The CDN may hand back a picture it re-compressed: the same picture, other bytes — kept, and
// never read as a change made here.
func TestRecompressedPictureIsKept(t *testing.T) {
	r := newRig(t)
	r.desk.herBlob("/photo.png", []byte("original png bytes"))
	row := r.desk.live("/photo.png")
	row.f.Mime = "image/png"
	r.desk.mu.Lock()
	r.desk.cdnOverride = map[string][]byte{row.f.URL: []byte("smaller png"), row.f.URL + "?nemini-v=" + row.f.SHA256[:16]: []byte("smaller png")}
	r.desk.mu.Unlock()
	r.pass()
	if s, ok := r.read("/photo.png"); !ok || s != "smaller png" {
		t.Fatalf("the re-compressed picture wasn't kept: %q %v", s, ok)
	}
	r.pass()
	if r.called("put /photo.png") {
		t.Fatal("the re-compressed picture went up as a change")
	}
	info, _ := os.Stat(r.local("/photo.png"))
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v, want 0644", info.Mode().Perm())
	}
}

func TestPastCacheGetsTheOriginal(t *testing.T) {
	r := newRig(t)
	r.desk.herBlob("/doc.png", []byte("the real bytes"))
	row := r.desk.live("/doc.png")
	row.f.Mime = "image/png"
	r.desk.mu.Lock()
	r.desk.cdnOverride = map[string][]byte{row.f.URL: []byte("cached other bytes")} // past the cache: the original
	r.desk.mu.Unlock()
	r.pass()
	if s, _ := r.read("/doc.png"); s != "the real bytes" {
		t.Fatalf("got %q, want the original", s)
	}
}

// Her desk full: new files stay in the folder only (the status says so), and go up once she
// makes room.
func TestFullDeskWaitsForRoom(t *testing.T) {
	r := newRig(t)
	r.desk.herText("/a.md", "a")
	r.desk.maxFiles = 1
	r.pass()
	r.write("/new.md", "waits for room")
	r.pass()
	if r.desk.live("/new.md") != nil {
		t.Fatal("went up past the quota")
	}
	if st := r.eng.Status(); st.State != "full" || st.Count != 1 {
		t.Fatalf("status = %+v, want full 1", st)
	}
	n := len(r.desk.calls)
	r.pass() // still full: not tried again
	if len(r.desk.calls) != n {
		t.Fatalf("tried again while full: %v", r.desk.calls[n:])
	}
	r.desk.herDelete("/a.md") // she makes room
	r.pass()
	if row := r.desk.live("/new.md"); row == nil || row.text != "waits for room" {
		t.Fatalf("didn't go up once there was room: %+v", row)
	}
	if st := r.eng.Status(); st.State != "up_to_date" {
		t.Fatalf("status after = %+v", st)
	}
}
