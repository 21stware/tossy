package main

import (
	"math"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/21stware/tossy/engine/trash"
)

// Emptying the trash, with motion on, sends a vehicle for it, the bigger the bin the bolder the
// ride (truckfx.go draws them):
//
//   - a desk basket or a waste basket: the cleaner, a matte-black van that backs up to the bin
//     and picks the items out with an arm off its roof;
//   - a pedal bin or a wheelie bin: an electric hauler that backs up and takes them in at the
//     back of its pod, its light bar filling as they go;
//   - a skip or a roll-off container: a hover craft that comes down over the bin and draws the
//     items up its tractor beam.
//
// The motion is the progress bar: an item leaves the bin when it has been deleted, and none of
// it runs ahead of the deletion or quicker than reads well.

const (
	truckArrive = 1100 * time.Millisecond
	truckLeave  = 950 * time.Millisecond
	truckHop    = 560 * time.Millisecond // an item's way into the vehicle
)

type vehicle int

const (
	vehVan vehicle = iota
	vehEV
	vehSkiff
)

// vehLen is each vehicle's length in its own units, front (left) to back.
var vehLen = [...]float32{236, 244, 224}

func vehicleFor(tier int) vehicle {
	switch {
	case tier <= 1:
		return vehVan
	case tier <= 3:
		return vehEV
	}
	return vehSkiff
}

// truckRun is one emptying. The fields under mu are written by the deleting goroutine; the
// rest belong to the view.
type truckRun struct {
	kind    vehicle
	preview bool // TOSSY_PREVIEW: deletion simulated, nothing goes (dev.go)
	t0      time.Time
	ents    []pileEnt // the pile as it was: frozen until the run ends
	planned int64
	totalB  int64
	totalN  int
	sizes   map[string]int64

	mu       sync.Mutex
	doneB    int64
	doneN    int
	cur      string // key being deleted
	curSize  int64
	curT0    time.Time
	finished []string // keys gone, not yet picked up by the view
	complete bool
	freed    int64
	left     []trash.Item
	err      error

	gTo     float64 // what the deletion has reached
	working string  // the key being deleted, as of this frame
	queue   []string
	next    time.Time
	gap     time.Duration
	hops    []truckBall
	out     map[string]bool // balls that left the pile
	leaveT0 time.Time
	rockT0  time.Time
	frontX  float32 // last frame's front, for the wheels and the speed lines
	paintT  time.Time
	speed   float32 // px/s, smoothed; negative going left
	wheel   float32 // the wheels' turn, radians
	shownB  float64 // the readout's counter
	hauledB int64   // bytes whose balls have gone in
	doneT0  time.Time
	puffs   []truckPuff
	puffT   time.Time
	ended   bool
}

type truckBall struct {
	size      int64
	sx, sy, r float32
	c         ui.Color
	t0        time.Time
}

type truckPuff struct {
	x, y float32
	t0   time.Time
}

// step takes EmptyWith's progress (from the deleting goroutine).
func (r *truckRun) step(s trash.Step) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !s.Done {
		r.cur, r.curSize, r.curT0 = s.Item.Orig, s.Item.Size, time.Time{} // timed from the frame that sees it
		return
	}
	r.cur = ""
	r.doneB += s.Item.Size
	r.doneN++
	if s.Err == nil {
		r.finished = append(r.finished, s.Item.Orig)
	}
}

// finish records EmptyWith's result (from the deleting goroutine).
func (r *truckRun) finish(freed int64, left []trash.Item, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.complete, r.freed, r.left, r.err = true, freed, left, err
}

// startTruck sets a run up, or returns nil when the bin is not on its stage (or motion is off):
// then the pile annihilates as before.
func (a *app) startTruck(keys map[string]bool, planned int64) *truckRun {
	f := &a.binS
	if !a.motion() || f.heroK < .99 || f.right == 0 {
		return nil
	}
	kind := vehicleFor(f.tier)
	if previewOn && previewKind >= 0 {
		kind = previewKind
	}
	r := &truckRun{kind: kind, preview: previewOn, t0: a.now, planned: planned, out: map[string]bool{}}
	r.ents = append([]pileEnt(nil), f.ents...)
	r.sizes = map[string]int64{}
	for _, e := range r.ents {
		if keys[e.key] {
			r.sizes[e.key] = e.size
			r.totalB += e.size
			r.totalN++
		}
	}
	r.gap = max(18*time.Millisecond, min(120*time.Millisecond, 1300*time.Millisecond/time.Duration(max(1, r.totalN))))
	f.truck = r
	a.frame = true
	return r
}

// truckGeo is the stage: the floor's y, the vehicles' scale, and the hero bin on screen.
type truckGeo struct {
	floor, sc                 float32
	binL, binR, binCX, binTop float32
}

func (a *app) truckGeo() truckGeo {
	f := &a.binS
	T := &binTiers[f.tier]
	ox, oy := f.origin()
	_, _, s, _, _ := f.hero()
	var g truckGeo
	g.binL, g.floor = f.toHero(ox+min(T.rx, T.l0), oy+T.h)
	g.binR, _ = f.toHero(ox+T.w, oy+T.h)
	g.binCX, g.binTop = f.toHero(ox+T.w/2, oy+T.ry)
	g.sc = max(96, min(168, T.h*s*1.1)) / 124
	return g
}

// truckParked is where the vehicle stands by the bin: its front's x and its base's y (the
// floor; the hover craft's belly).
func (a *app) truckParked(r *truckRun, g truckGeo) (front, base float32) {
	L := vehLen[r.kind] * g.sc
	switch r.kind {
	case vehVan:
		return g.binL - 6*g.sc - L, g.floor
	case vehEV:
		return g.binL - 34*g.sc - L, g.floor
	}
	return g.binCX - 178*g.sc, g.binTop - 100*g.sc // the hatch over the bin
}

// truckPose is where the vehicle is this frame: backing (or coming down) in, parked, or
// driving (or flying) off to the left.
func (a *app) truckPose(r *truckRun, g truckGeo) (front, base float32) {
	pf, pb := a.truckParked(r, g)
	L := vehLen[r.kind] * g.sc
	el := a.now.Sub(r.t0)
	if el < truckArrive {
		k := float32(easeStd(float64(el) / float64(truckArrive)))
		if r.kind == vehSkiff {
			return -L - 40 + (pf+L+40)*k, pb - 170*g.sc*(1-k)
		}
		return -L - 12 + (pf+L+12)*k, pb
	}
	if !r.leaveT0.IsZero() && a.now.After(r.leaveT0) {
		k := float32(easeIn(min(1, float64(a.now.Sub(r.leaveT0))/float64(truckLeave))))
		if r.kind == vehSkiff {
			return pf + (-L-80-pf)*k, pb - 240*g.sc*k
		}
		return pf + (-L-12-pf)*k, pb
	}
	return pf, pb
}

// target is the progress the deletion has reached by now, the frame's time: by bytes and by
// count, with the item in hand creeping on so a long one still moves.
func (r *truckRun) target(now time.Time) (float64, string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fin := r.finished
	r.finished = nil
	if r.complete {
		return 1, "", fin
	}
	byB := func(n int64) float64 { return float64(n) / float64(max(1, r.totalB)) }
	g := .3 * float64(r.doneN) / float64(max(1, r.totalN))
	if r.totalB > 0 {
		g += .7 * byB(r.doneB)
	} else {
		g /= .3
	}
	if r.cur != "" {
		if r.curT0.IsZero() {
			r.curT0 = now
		}
		el := now.Sub(r.curT0).Seconds()
		part := .3 / float64(max(1, r.totalN))
		if r.totalB > 0 {
			part += .7 * byB(r.curSize)
		}
		g += part * .9 * (1 - math.Exp(-el/1.4))
	}
	return min(g, .97), r.cur, fin
}

// stepTruck advances the run one frame; it ends the run (and calls emptied) once the vehicle
// has gone.
func (a *app) stepTruck() {
	f := &a.binS
	r := f.truck
	if r == nil || r.ended {
		return
	}
	a.frame = true
	now := a.now
	g, cur, fin := r.target(now)
	r.gTo, r.working = g, cur
	el := now.Sub(r.t0)
	// item by item: the balls of what is gone go in, one after another
	for _, k := range fin {
		if _, ok := f.pile[k]; ok && !r.out[k] {
			r.queue = append(r.queue, k)
		}
	}
	if el < truckArrive+200*time.Millisecond {
		return
	}
	for len(r.queue) > 0 && !now.Before(r.next) {
		k := r.queue[0]
		r.queue = r.queue[1:]
		b := f.pile[k]
		ox, oy := f.origin()
		_, _, s, _, _ := f.hero()
		x, y := f.toHero(ox+b.x, oy+b.y)
		r.hops = append(r.hops, truckBall{r.sizes[k], x, y, b.r * s, a.pal.level[b.lv], now})
		r.out[k] = true
		if r.next.Before(now.Add(-r.gap)) {
			r.next = now
		}
		r.next = r.next.Add(r.gap)
	}
	if r.leaveT0.IsZero() && g >= 1 && len(r.queue) == 0 && len(r.hops) == 0 {
		r.leaveT0 = now.Add(420 * time.Millisecond) // a beat to show the total before it goes
	}
	if !r.leaveT0.IsZero() && now.Sub(r.leaveT0) >= truckLeave {
		a.endTruck()
	}
}

// done says the work is over: the total shows, and the vehicle is about to go.
func (r *truckRun) done() bool { return !r.leaveT0.IsZero() }

// endTruck finishes the run: the screen catches up with the deletion.
func (a *app) endTruck() {
	f := &a.binS
	r := f.truck
	r.ended = true
	f.truck = nil
	r.mu.Lock()
	freed, left, err := r.freed, r.left, r.err
	r.mu.Unlock()
	if r.preview { // the bin ends empty as after a real one; the files are still there (dev.go)
		left = a.previewUnmark(r)
	}
	a.emptied(freed, r.planned, left, err)
}

// truckLean is a wheeled body on its springs: lean (units at the roof, + to the right) and a
// squash. Braking, the body runs on and settles back; pulling away, it sits back.
func (a *app) truckLean(r *truckRun) (lean, squash float32) {
	squash = 1
	kick := func(t0 time.Time, amp float64, start bool) {
		if t0.IsZero() {
			return
		}
		t := a.now.Sub(t0).Seconds()
		if t < 0 || t > 1.4 {
			return
		}
		e := math.Exp(-t / .24)
		w := math.Cos(t * 15)
		if start {
			w = math.Sin(t * 15)
		}
		lean += float32(amp * w * e)
		squash -= float32(.03 * math.Abs(math.Cos(t*15)) * e)
	}
	kick(r.t0.Add(truckArrive), 5, false)
	kick(r.leaveT0, 5, true)
	return
}
