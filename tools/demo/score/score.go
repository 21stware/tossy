// Package score writes the promo film's music: a synthesised track, built from the film's own
// cue sheet. Nothing is sampled: the drums, the bass, the pads and the rest are oscillators,
// noise and filters, mixed and limited here.
//
// The track is one piece on one grid. It keeps 120 beats a minute from its first bar to its
// last, over four chords that turn every four bars, and a figure in eighths that never stops.
// It changes in one way only: it grows. Each section of the film adds a part to the ones
// already playing (the kick, the bass, the hats, the arpeggio, the melody) and none is taken
// away before the end; the filters open with the film's time, however long it is, not with its
// cuts. A snare roll leads into the film's two big moments, and a hit lands on the few cuts
// that ask for one.
//
// The film (app/promo_video_test.go) cuts on the beat and starts its sections on the bar;
// Render returns the track and WriteWAV saves it for tools/demo/mux.swift to lay under the
// picture.
package score

import (
	"encoding/binary"
	"math"
	"os"
)

// SR is the sample rate.
const SR = 48000

// A Section of the film starts at T seconds and runs to the next; its name picks the music.
type Section struct {
	Name string
	T    float64
}

// A Hit is a cut that flashes, K strong. Only the strong ones (K from .2) sound.
type Hit struct{ T, K float64 }

// Cues is the film's cue sheet.
type Cues struct {
	Sections []Section
	Hits     []Hit
	Ticks    []float64 // the cold open's flurry: a cut each, in quarters, eighths, sixteenths
	End      float64   // the film's length
}

const (
	beat = .5       // seconds, at 120 a minute
	step = beat / 4 // a sixteenth
	bar  = 4 * beat // four beats
	tau  = 2 * math.Pi
)

// bus is a stereo mix.
type bus [2][]float64

func (b *bus) add(i int, l, r float64) {
	if i >= 0 && i < len(b[0]) {
		b[0][i] += l
		b[1][i] += r
	}
}

type eng struct {
	n     int
	drums bus // dry, never ducked
	music bus // ducked under the kick
	fx    bus // impacts and risers
	rev   bus // send to the reverb
	dly   bus // send to the delay
	kicks []float64
	seed  uint32
	side  float64 // the hats go from side to side
}

// rnd is white noise, -1 to 1, the same on every run.
func (e *eng) rnd() float64 {
	e.seed = e.seed*1664525 + 1013904223
	return float64(e.seed>>8)/float64(1<<23) - 1
}

func hz(midi float64) float64 { return 440 * math.Pow(2, (midi-69)/12) }

// saw is a band-limited sawtooth at phase ph (0 to 1) stepping dt a sample.
func saw(ph, dt float64) float64 {
	v := 2*ph - 1
	switch {
	case ph < dt:
		x := ph / dt
		v -= x + x - x*x - 1
	case ph > 1-dt:
		x := (ph - 1) / dt
		v -= x*x + x + x + 1
	}
	return v
}

// svf is a state-variable filter.
type svf struct{ a, b float64 }

// run filters x at cutoff (Hz) and resonance q, and returns the low, band and high outputs.
func (f *svf) run(x, cutoff, q float64) (lp, bp, hp float64) {
	g := math.Tan(math.Pi * math.Min(cutoff, SR*.45) / SR)
	k := 1 / q
	a1 := 1 / (1 + g*(g+k))
	a2 := g * a1
	a3 := g * a2
	v3 := x - f.b
	v1 := a1*f.a + a2*v3
	v2 := f.b + a2*f.a + a3*v3
	f.a, f.b = 2*v1-f.a, 2*v2-f.b
	return v2, v1, x - k*v1 - v2
}

func pan(p float64) (l, r float64) {
	a := (p + 1) * math.Pi / 4
	return math.Cos(a), math.Sin(a)
}

// ---------------------------------------------------------------------------------------------
// drums

func (e *eng) kick(t, v float64) {
	i0, ph := int(t*SR), 0.
	for i := 0; i < int(.42*SR); i++ {
		tt := float64(i) / SR
		ph += (44 + 130*math.Exp(-tt/.026)) / SR
		a := math.Exp(-tt / .17)
		if tt > .32 {
			a *= 1 - (tt-.32)/.1
		}
		s := math.Sin(tau*ph) * a
		if i < 120 {
			s += e.rnd() * .22 * (1 - float64(i)/120)
		}
		s = math.Tanh(s*1.6) * .8 * v
		e.drums.add(i0+i, s, s)
	}
	e.kicks = append(e.kicks, t)
}

func (e *eng) hat(t, v float64, open bool) {
	i0, dec := int(t*SR), .028
	if open {
		dec = .11
	}
	var f svf
	e.side = -e.side
	l, r := pan(.35 * e.side)
	for i := 0; i < int(dec*6*SR); i++ {
		tt := float64(i) / SR
		_, _, hp := f.run(e.rnd(), 8500, .8)
		s := hp * math.Exp(-tt/dec) * v
		e.drums.add(i0+i, s*l, s*r)
	}
}

func (e *eng) clap(t, v float64) {
	i0 := int(t * SR)
	var f svf
	for i := 0; i < int(.4*SR); i++ {
		tt := float64(i) / SR
		env := 0.
		for _, d := range []float64{0, .011, .023} {
			if tt >= d {
				env += math.Exp(-(tt - d) / .007)
			}
		}
		if tt >= .023 {
			env += .7 * math.Exp(-(tt-.023)/.085)
		}
		_, bp, _ := f.run(e.rnd(), 1700, 1.1)
		s := bp * env * v * .9
		e.drums.add(i0+i, s, s)
		e.rev.add(i0+i, s*.35, s*.35)
	}
}

func (e *eng) snare(t, v float64) {
	i0, ph := int(t*SR), 0.
	var f svf
	for i := 0; i < int(.22*SR); i++ {
		tt := float64(i) / SR
		ph += (175 + 60*math.Exp(-tt/.02)) / SR
		_, bp, _ := f.run(e.rnd(), 3200, .9)
		s := (bp*math.Exp(-tt/.06) + .5*math.Sin(tau*ph)*math.Exp(-tt/.05)) * v
		e.drums.add(i0+i, s, s)
		e.rev.add(i0+i, s*.2, s*.2)
	}
}

// ---------------------------------------------------------------------------------------------
// pitched

// bass is a note of the bass: a saw over a sine, low-passed, its filter closing after the attack.
func (e *eng) bass(t, midi, dur, v, cut float64) {
	i0, f0 := int(t*SR), hz(midi)
	var ph, ph2 float64
	var f svf
	n := int((dur + .04) * SR)
	for i := 0; i < n; i++ {
		tt := float64(i) / SR
		ph += f0 / SR
		ph -= math.Floor(ph)
		ph2 += f0 / 2 / SR
		a := math.Min(1, tt/.004)
		if tt > dur {
			a *= math.Max(0, 1-(tt-dur)/.04)
		}
		lp, _, _ := f.run(saw(ph, f0/SR), cut*(1+2.5*math.Exp(-tt/.07)), 1.3)
		s := (lp*.7 + math.Sin(tau*ph2)*.6) * a * v
		e.music.add(i0+i, s, s)
	}
}

// pad holds a chord: three detuned saws a note, spread wide, under a filter that moves from
// c0 to c1 over the note.
func (e *eng) pad(t float64, notes []float64, dur, v, c0, c1, attack float64) {
	i0 := int(t * SR)
	type osc struct{ ph, dt, l, r float64 }
	var os []osc
	for k, m := range notes {
		for d := -1; d <= 1; d++ {
			f := hz(m + float64(d)*.09)
			l, r := pan(float64(d)*.75 + (float64(k%2)-.5)*.3)
			os = append(os, osc{e.rnd()*.5 + .5, f / SR, l, r})
		}
	}
	var fl, fr svf
	rel := .7
	n := int((dur + rel) * SR)
	g := v / math.Sqrt(float64(len(notes)))
	for i := 0; i < n; i++ {
		tt := float64(i) / SR
		var l, r float64
		for k := range os {
			o := &os[k]
			o.ph += o.dt
			if o.ph >= 1 {
				o.ph--
			}
			s := saw(o.ph, o.dt)
			l += s * o.l
			r += s * o.r
		}
		a := math.Min(1, tt/attack)
		if tt > dur {
			a *= math.Max(0, 1-(tt-dur)/rel)
		}
		c := c0 + (c1-c0)*math.Min(1, tt/dur)
		l, _, _ = fl.run(l, c, .8)
		r, _, _ = fr.run(r, c, .8)
		l, r = l*a*g*.5, r*a*g*.5
		e.music.add(i0+i, l, r)
		e.rev.add(i0+i, l*.5, r*.5)
	}
}

// stab is a chord struck and let go.
func (e *eng) stab(t float64, notes []float64, v float64) {
	e.pad(t, notes, .16, v, 5200, 700, .004)
}

// pluck is a short bright note, into the delay.
func (e *eng) pluck(t, midi, v, p, bright float64) {
	i0, f0 := int(t*SR), hz(midi)
	var ph float64
	var f svf
	l, r := pan(p)
	for i := 0; i < int(.55*SR); i++ {
		tt := float64(i) / SR
		ph += f0 / SR
		ph -= math.Floor(ph)
		sq := 1.
		if ph > .5 {
			sq = -1
		}
		lp, _, _ := f.run(saw(ph, f0/SR)*.7+sq*.3, 700+bright*math.Exp(-tt/.11), 2.2)
		s := lp * math.Exp(-tt/.17) * math.Min(1, tt/.002) * v
		e.music.add(i0+i, s*l, s*r)
		e.dly.add(i0+i, s*r*.4, s*l*.4)
		e.rev.add(i0+i, s*.22, s*.22)
	}
}

// lead is the melody's voice: two saws and a sine, a little vibrato once the note has settled.
func (e *eng) lead(t, midi, dur, v float64) {
	i0 := int(t * SR)
	var p1, p2, p3 float64
	var f svf
	n := int((dur + .14) * SR)
	for i := 0; i < n; i++ {
		tt := float64(i) / SR
		vib := .13 * math.Min(1, math.Max(0, tt-.18)/.3) * math.Sin(tau*5.4*tt)
		f0 := hz(midi + vib)
		p1 += f0 * 1.0035 / SR
		p1 -= math.Floor(p1)
		p2 += f0 * .9965 / SR
		p2 -= math.Floor(p2)
		p3 += f0 / SR
		a := math.Min(1, tt/.012)
		if tt > dur {
			a *= math.Max(0, 1-(tt-dur)/.14)
		}
		lp, _, _ := f.run(saw(p1, f0/SR)+saw(p2, f0/SR), 2400+1500*math.Exp(-tt/.2), 1)
		s := (lp*.3 + math.Sin(tau*p3)*.35) * a * v
		e.music.add(i0+i, s, s)
		e.rev.add(i0+i, s*.4, s*.4)
		e.dly.add(i0+i, s*.22, s*.22)
	}
}

// drone is a low held note that fades in and out.
func (e *eng) drone(t0, t1, midi, v float64) {
	i0, f0 := int(t0*SR), hz(midi)
	var ph, ph2 float64
	var f svf
	for i := 0; i < int((t1-t0)*SR); i++ {
		tt := float64(i) / SR
		ph += f0 / SR
		ph -= math.Floor(ph)
		ph2 += f0 * 1.004 / SR
		ph2 -= math.Floor(ph2)
		a := math.Min(1, tt/.9) * math.Min(1, (t1-t0-tt)/.5)
		lp, _, _ := f.run(saw(ph, f0/SR)+saw(ph2, f0/SR), 170+60*math.Sin(tau*.21*tt), .8)
		s := (lp*.5 + math.Sin(tau*ph)*.5) * a * v
		e.fx.add(i0+i, s, s)
	}
}

// ---------------------------------------------------------------------------------------------
// effects

// impact is a cut that lands: a falling sub, a burst of noise closing down, a long tail.
func (e *eng) impact(t, k float64) {
	i0, ph := int(t*SR), 0.
	var fl, fr svf
	for i := 0; i < int(2.2*SR); i++ {
		tt := float64(i) / SR
		ph += (36 + 70*math.Exp(-tt/.11)) / SR
		sub := math.Sin(tau*ph) * math.Exp(-tt/.75) * .9
		c := 260 + 4200*math.Exp(-tt/.3)
		l, _, _ := fl.run(e.rnd(), c, .7)
		r, _, _ := fr.run(e.rnd(), c, .7)
		a := math.Exp(-tt/.42) * .55 * math.Min(1, tt/.002)
		l, r = (sub+l*a)*k, (sub+r*a)*k
		e.fx.add(i0+i, l, r)
		e.rev.add(i0+i, l*.3, r*.3)
	}
	e.kicks = append(e.kicks, t)
}

// riser is noise swept up under a rising tone, from t0, cut off at t1.
func (e *eng) riser(t0, t1, v float64) {
	i0 := int(t0 * SR)
	var fl, fr svf
	var ph float64
	n := int((t1 - t0) * SR)
	for i := 0; i < n; i++ {
		p := float64(i) / float64(n)
		c := 240 * math.Pow(36, p)
		_, l, _ := fl.run(e.rnd(), c, 2.6)
		_, r, _ := fr.run(e.rnd(), c*1.07, 2.6)
		ph += hz(57+24*p*p) / SR
		a := p * p * v * .7 * math.Min(1, float64(n-i)/(.012*SR))
		tone := math.Sin(tau*ph) * .25
		l, r = (l+tone)*a, (r+tone)*a
		e.fx.add(i0+i, l, r)
		e.rev.add(i0+i, l*.3, r*.3)
	}
}

// ---------------------------------------------------------------------------------------------
// the arrangement

// A chord is its bass note and the notes the pad and the arpeggio take.
type chord struct {
	root  float64
	notes []float64
}

var (
	am = chord{33, []float64{57, 60, 64, 71}} // A minor, add 9
	fM = chord{29, []float64{53, 57, 60, 67}} // F, add 9
	cM = chord{36, []float64{55, 60, 64, 67}} // C
	gM = chord{31, []float64{55, 59, 62, 69}} // G, add 9
)

// The melody, in eighths, a bar to a chord; 0 holds or rests.
var tune = map[*chord][8]float64{
	&am: {76, 0, 72, 74, 76, 0, 0, 79},
	&fM: {81, 0, 79, 0, 76, 0, 74, 72},
	&cM: {76, 0, 72, 74, 76, 0, 79, 0},
	&gM: {74, 0, 71, 74, 79, 0, 0, 0},
}

// A part is what plays in a section of the film. Each section's is the one before it and
// something more, to the end.
type part struct {
	kick  float64 // how hard, 0 for none
	bass  bool    // a note a bar
	drive bool    // the bass in eighths
	hats  bool
	clap  bool
	arp   bool // the figure in sixteenths
	lead  bool
	stabs bool
	roll  bool // a snare roll through it, into what comes next
}

var parts = map[string]part{
	"open":   {kick: .7, bass: true},
	"full":   {kick: .85, bass: true},
	"flurry": {kick: .85, bass: true},
	"scan":   {kick: .95, drive: true, hats: true},
	"see":    {kick: .95, drive: true, hats: true, clap: true, arp: true},
	"click":  {kick: .95, drive: true, hats: true, clap: true, arp: true, roll: true},
	"throw":  {kick: .95, drive: true, hats: true, clap: true, arp: true, lead: true},
	"undo":   {kick: .95, drive: true, hats: true, clap: true, arp: true, lead: true, roll: true},
	"haul":   {kick: .95, drive: true, hats: true, clap: true, arp: true, lead: true, stabs: true},
	"payoff": {kick: .95, drive: true, hats: true, clap: true, arp: true, lead: true, stabs: true},
}

func (e *eng) arrange(c Cues) {
	end, lift := c.End, c.End // where the last chord falls, and where the chords turn to the major
	for _, s := range c.Sections {
		switch s.Name {
		case "end":
			end = s.T
		case "payoff":
			lift = s.T
		}
	}
	at := func(t float64) (name string, t0, t1 float64) {
		t1 = c.End
		for i, s := range c.Sections {
			if s.T > t+1e-6 {
				t1 = s.T
				break
			}
			name, t0 = c.Sections[i].Name, s.T
		}
		return
	}
	// the rolls: each run of sections that asks for one is one roll
	type span struct{ t0, t1 float64 }
	var rolls []span
	for i, s := range c.Sections {
		if !parts[s.Name].roll {
			continue
		}
		t1 := c.End
		if i+1 < len(c.Sections) {
			t1 = c.Sections[i+1].T
		}
		if n := len(rolls); n > 0 && rolls[n-1].t1 == s.T {
			rolls[n-1].t1 = t1
		} else {
			rolls = append(rolls, span{s.T, t1})
		}
	}
	main, major := []*chord{&am, &fM, &cM, &gM}, []*chord{&cM, &gM, &am, &fM}
	figure := [8]int{0, 2, 1, 2, 3, 2, 1, 2} // the eighths, over the chord's notes

	e.drone(0, math.Min(8, end), 33, .4)
	for s := 0; float64(s)*step < end-1e-6; s++ {
		t := float64(s) * step
		b, k := s/16, s%16
		name, _, _ := at(t)
		p := parts[name]
		ch := main[b%4]
		if t >= lift-1e-6 {
			ch = major[int(math.Round((t-lift)/bar-.5+1e-6))%4]
		}
		left := end - t
		open := math.Min(1, t/(.85*end)) // the filters open with the film

		if k == 0 {
			e.pad(t, ch.notes, math.Min(bar, left)-.08, .3, 600+1500*open, 1100+3200*open, .1)
			if p.bass && !p.drive {
				e.bass(t, ch.root, math.Min(bar, left)-.2, .45, 220+200*open)
			}
		}
		if k%2 == 0 { // the figure, from the first bar to the last
			e.pluck(t, ch.notes[figure[k/2]]+12, .24, float64(k/2%2)*1.2-.6, 1600+3600*open)
		} else if p.arp {
			e.pluck(t, ch.notes[(k/2+1)%4]+24, .13, float64(k/2%2)*-1.4+.7, 2400+3600*open)
		}
		if p.kick > 0 && k%4 == 0 {
			e.kick(t, p.kick)
		}
		if p.hats {
			if k%4 == 2 {
				e.hat(t, .4, true)
			} else {
				e.hat(t, .13+.07*float64(k%2), false)
			}
		}
		if p.clap && (k == 4 || k == 12) {
			e.clap(t, .42)
		}
		if p.drive && k%2 == 0 {
			m := ch.root
			if k%8 == 6 {
				m += 12
			}
			e.bass(t, m, step*1.5, .42, 260+420*open)
		}
		if p.lead && k%2 == 0 {
			if m := tune[ch][k/2]; m > 0 {
				d := step * 2
				for j := k/2 + 1; j < 8 && tune[ch][j] == 0; j++ {
					d += step * 2
				}
				e.lead(t, m, math.Min(d, left)*.92, .32)
			}
		}
		if p.stabs && k%8 == 0 {
			e.stab(t, ch.notes, .3)
		}
		for _, r := range rolls { // eighths, then sixteenths, getting louder
			if q := (t - r.t0) / (r.t1 - r.t0); t >= r.t0-1e-6 && t < r.t1-1e-6 && (q >= .5 || k%2 == 0) {
				e.snare(t, .14+.3*q)
			}
		}
	}
	for _, r := range rolls {
		e.riser(r.t0, r.t1, .45)
	}
	// the cold open's flurry is the film's own roll: a snare a cut
	for k, t := range c.Ticks {
		e.snare(t, .16+.3*float64(k)/float64(max(1, len(c.Ticks)-1)))
	}
	if n := len(c.Ticks); n > 0 {
		e.riser(c.Ticks[0], c.Ticks[n-1]+step, .4)
	}
	// home: C, held, an arpeggio up and away
	if end < c.End {
		e.pad(end, []float64{36, 48, 55, 60, 64, 67, 74}, c.End-end-1.4, .55, 3600, 600, .004)
		e.bass(end, 24, 2.5, .5, 300)
		for k, m := range []float64{72, 76, 79, 84, 86, 88, 91, 96} {
			e.pluck(end+float64(k)*step, m, .26-.015*float64(k), float64(k%2)*1.4-.7, 5000)
		}
	}
	for _, h := range c.Hits {
		if h.K >= .2 {
			e.impact(h.T, .8)
		}
	}
}

// ---------------------------------------------------------------------------------------------
// the mix

// reverb is eight combs and four all-passes a side, after Freeverb.
func reverb(in bus) bus {
	n := len(in[0])
	out := bus{make([]float64, n), make([]float64, n)}
	combs := []int{1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617}
	alls := []int{556, 441, 341, 225}
	for ch := 0; ch < 2; ch++ {
		acc := make([]float64, n)
		for _, d := range combs {
			d = (d + ch*23) * SR / 44100
			buf := make([]float64, d)
			var lp float64
			for i := 0; i < n; i++ {
				y := buf[i%d]
				lp = y*.72 + lp*.28
				buf[i%d] = (in[0][i]+in[1][i])*.5*.03 + lp*.86
				acc[i] += y
			}
		}
		for _, d := range alls {
			d = (d + ch*23) * SR / 44100
			buf := make([]float64, d)
			for i := 0; i < n; i++ {
				b := buf[i%d]
				y := b - acc[i]
				buf[i%d] = acc[i] + b*.5
				acc[i] = y
			}
		}
		out[ch] = acc
	}
	return out
}

// delay is a dotted eighth, bouncing from side to side.
func delay(in bus) bus {
	n := len(in[0])
	out := bus{make([]float64, n), make([]float64, n)}
	d := int(.375 * SR)
	var ll, lr float64
	for i := d; i < n; i++ {
		l := in[0][i-d] + out[1][i-d]*.42
		r := in[1][i-d] + out[0][i-d]*.42
		ll += (l - ll) * .35
		lr += (r - lr) * .35
		out[0][i], out[1][i] = ll, lr
	}
	return out
}

// Render writes the track for the cue sheet: two channels of samples, -1 to 1.
func Render(c Cues) (left, right []float32) {
	n := int((c.End + .1) * SR)
	mk := func() bus { return bus{make([]float64, n), make([]float64, n)} }
	e := &eng{n: n, drums: mk(), music: mk(), fx: mk(), rev: mk(), dly: mk(), seed: 7, side: 1}
	e.arrange(c)

	// the music ducks under each kick and each impact
	duck := make([]float64, n)
	for _, t := range e.kicks {
		i0 := int(t * SR)
		for i := 0; i < int(.4*SR) && i0+i < n; i++ {
			if i0+i < 0 {
				continue
			}
			tt := float64(i) / SR
			duck[i0+i] = math.Max(duck[i0+i], math.Exp(-tt/.13)*math.Min(1, tt/.004))
		}
	}
	wet, echo := reverb(e.rev), delay(e.dly)
	left, right = make([]float32, n), make([]float32, n)
	var hl, hr, sl, sr svf
	peak := 0.
	mix := [2][]float64{make([]float64, n), make([]float64, n)}
	for i := 0; i < n; i++ {
		g := 1 - .6*duck[i]
		l := e.drums[0][i] + (e.music[0][i]+echo[0][i]*.8)*g + e.fx[0][i]*.8 + wet[0][i]*(.6+.4*g)*1.3
		r := e.drums[1][i] + (e.music[1][i]+echo[1][i]*.8)*g + e.fx[1][i]*.8 + wet[1][i]*(.6+.4*g)*1.3
		_, _, l = hl.run(l, 28, .7) // nothing below what a speaker can say
		_, _, r = hr.run(r, 28, .7)
		_, _, al := sl.run(l, 4500, .7) // and some air on top
		_, _, ar := sr.run(r, 4500, .7)
		l, r = l+al*.9, r+ar*.9
		// out with the picture
		if t := float64(i) / SR; t > c.End-.7 {
			f := math.Max(0, (c.End-t)/.7)
			l, r = l*f, r*f
		}
		l, r = math.Tanh(l*.85), math.Tanh(r*.85) // the loud parts lean on the limiter
		mix[0][i], mix[1][i] = l, r
		peak = math.Max(peak, math.Max(math.Abs(l), math.Abs(r)))
	}
	k := .89 / math.Max(peak, 1e-9)
	for i := 0; i < n; i++ {
		left[i], right[i] = float32(mix[0][i]*k), float32(mix[1][i]*k)
	}
	return left, right
}

// WriteWAV saves the track as 16-bit stereo.
func WriteWAV(path string, left, right []float32) error {
	n := len(left)
	b := make([]byte, 44+n*4)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n*4))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 2)
	binary.LittleEndian.PutUint32(b[24:], SR)
	binary.LittleEndian.PutUint32(b[28:], SR*4)
	binary.LittleEndian.PutUint16(b[32:], 4)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n*4))
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(b[44+i*4:], uint16(int16(math.Round(float64(left[i])*32767))))
		binary.LittleEndian.PutUint16(b[46+i*4:], uint16(int16(math.Round(float64(right[i])*32767))))
	}
	return os.WriteFile(path, b, 0o644)
}
