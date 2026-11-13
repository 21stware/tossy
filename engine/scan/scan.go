package scan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Options control a scan.
type Options struct {
	// Workers is the number of concurrent directory readers. 0 = sensible default for the platform.
	Workers int
	// CrossDevice descends into other mounted filesystems. Default false (like du -x).
	CrossDevice bool
	// Exclude lists absolute paths that are recorded but not descended. Platform defaults
	// (pseudo filesystems, macOS firmlink duplicates…) are always added.
	Exclude []string
	// Progress, if set, is updated live with atomic counters.
	Progress *Progress
}

// Progress counters, safe to read concurrently while a scan runs.
type Progress struct {
	Files, Dirs, Bytes, Errors atomic.Int64
	current                    atomic.Pointer[string]
}

// Current returns a recently visited directory (for display).
func (p *Progress) Current() string {
	if s := p.current.Load(); s != nil {
		return *s
	}
	return ""
}

// rawEntry is what a platform directory lister yields for one entry.
type rawEntry struct {
	name  string
	dir   bool
	link  bool
	size  int64 // allocated bytes
	mtime uint32
	dev   uint64
	ino   uint64
	nlink uint32
	err   bool
	cloud bool // placeholder whose data lives in the cloud (not descended, size is local only)
}

type job struct {
	idx  int32
	path string
}

type scanner struct {
	ctx  context.Context
	opt  Options
	prog *Progress

	qmu     sync.Mutex
	qcond   *sync.Cond
	queue   []job
	pending int
	done    bool

	tmu  sync.Mutex
	tree *Tree

	hmu   sync.Mutex
	links map[[2]uint64]struct{}

	devs     map[uint64]bool
	excluded map[string]bool
	files    atomic.Int64
	dirs     atomic.Int64
	errs     atomic.Int64
	hard     atomic.Int64
}

// Scan walks root and returns the tree. On cancellation it returns the partial tree and ctx.Err().
func Scan(ctx context.Context, root string, opt Options) (*Tree, error) {
	t0 := time.Now()
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if opt.Workers <= 0 {
		opt.Workers = defaultWorkers()
	}
	if opt.Progress == nil {
		opt.Progress = &Progress{}
	}
	s := &scanner{
		ctx: ctx, opt: opt, prog: opt.Progress,
		tree:     &Tree{Root: abs, Nodes: make([]Node, 0, 1<<16), names: make([]byte, 0, 1<<20)},
		links:    map[[2]uint64]struct{}{},
		devs:     map[uint64]bool{},
		excluded: map[string]bool{},
	}
	s.qcond = sync.NewCond(&s.qmu)
	for _, d := range rootDevices(abs) {
		s.devs[d] = true
	}
	for _, e := range append(defaultExcludes(abs), opt.Exclude...) {
		if e = filepath.Clean(e); e != filepath.Clean(abs) {
			s.excluded[e] = true
		}
	}
	// root node
	s.tree.names = append(s.tree.names, abs...)
	s.tree.Nodes = append(s.tree.Nodes, Node{nameLen: uint16(min(len(abs), 65535)), Flags: FlagDir, Parent: None, First: None, Next: None, MTime: uint32(fi.ModTime().Unix())})
	if !fi.IsDir() {
		s.tree.Nodes[0].Flags = 0
		s.tree.Nodes[0].Size = fi.Size()
	} else {
		s.push(job{0, abs})
		var wg sync.WaitGroup
		for i := 0; i < opt.Workers; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); s.work() }()
		}
		wg.Wait()
	}
	t := s.tree
	t.finish()
	t.Stats = Stats{Files: s.files.Load(), Dirs: s.dirs.Load(), Errors: s.errs.Load(), Hardlinks: s.hard.Load(), Bytes: t.Nodes[0].Size, Millis: time.Since(t0).Milliseconds()}
	return t, ctx.Err()
}

func (s *scanner) push(j job) {
	s.qmu.Lock()
	s.queue = append(s.queue, j)
	s.pending++
	s.qmu.Unlock()
	s.qcond.Signal()
}

func (s *scanner) pushMany(js []job) {
	if len(js) == 0 {
		return
	}
	s.qmu.Lock()
	s.queue = append(s.queue, js...)
	s.pending += len(js)
	s.qmu.Unlock()
	if len(js) == 1 {
		s.qcond.Signal()
	} else {
		s.qcond.Broadcast()
	}
}

func (s *scanner) work() {
	var ents []rawEntry
	var sub []job
	for {
		s.qmu.Lock()
		for len(s.queue) == 0 && !s.done {
			s.qcond.Wait()
		}
		if s.done {
			s.qmu.Unlock()
			return
		}
		// LIFO: depth-first keeps the queue (and memory) small
		j := s.queue[len(s.queue)-1]
		s.queue = s.queue[:len(s.queue)-1]
		s.qmu.Unlock()

		sub = sub[:0]
		ents = ents[:0]
		if s.ctx.Err() != nil {
			s.tmu.Lock()
			s.tree.Nodes[j.idx].Flags |= FlagPartial
			s.tmu.Unlock()
		} else {
			sub = s.visit(j, &ents, sub)
		}
		s.pushMany(sub)

		s.qmu.Lock()
		s.pending--
		if s.pending == 0 {
			s.done = true
			s.qcond.Broadcast()
		}
		s.qmu.Unlock()
	}
}

// visit lists one directory, appends its entries to the tree and returns subdirectories to descend.
func (s *scanner) visit(j job, ents *[]rawEntry, sub []job) []job {
	p := j.path
	s.prog.current.Store(&p)
	t0 := time.Now()
	err := listDir(j.path, ents)
	if debugSlow && time.Since(t0) > 200*time.Millisecond {
		fmt.Fprintf(os.Stderr, "slow %v %d %s %v\n", time.Since(t0).Round(time.Millisecond), len(*ents), j.path, err)
	}
	s.dirs.Add(1)
	s.prog.Dirs.Add(1)
	if err != nil && len(*ents) == 0 {
		s.errs.Add(1)
		s.prog.Errors.Add(1)
		s.tmu.Lock()
		s.tree.Nodes[j.idx].Flags |= FlagErr
		s.tmu.Unlock()
		return sub
	}
	var files, bytes, errs int64
	// hard-link dedupe outside the tree lock
	for k := range *ents {
		e := &(*ents)[k]
		if e.err {
			errs++
		}
		if !e.dir && e.nlink > 1 {
			key := [2]uint64{e.dev, e.ino}
			s.hmu.Lock()
			_, seen := s.links[key]
			if !seen {
				s.links[key] = struct{}{}
			}
			s.hmu.Unlock()
			if seen {
				e.size = -1 // marker: counted elsewhere
			}
		}
	}
	sep := string(filepath.Separator)
	base := j.path
	if !strings.HasSuffix(base, sep) {
		base += sep
	}

	s.tmu.Lock()
	t := s.tree
	for k := range *ents {
		e := &(*ents)[k]
		n := Node{nameOff: uint32(len(t.names)), nameLen: uint16(min(len(e.name), 65535)), Parent: j.idx, First: None, Next: t.Nodes[j.idx].First, MTime: e.mtime, Size: e.size}
		t.names = append(t.names, e.name[:n.nameLen]...)
		if e.size < 0 {
			n.Size = 0
			n.Flags |= FlagHardlink
			s.hard.Add(1)
		}
		if e.err {
			n.Flags |= FlagErr
		}
		if e.link {
			n.Flags |= FlagSymlink
		}
		if e.cloud {
			n.Flags |= FlagCloud
		}
		idx := int32(len(t.Nodes))
		if e.dir {
			n.Flags |= FlagDir
			full := base + e.name
			if e.cloud {
				// dataless folder: listing it would trigger a download
			} else if (!s.opt.CrossDevice && !s.devs[e.dev]) || s.excluded[full] {
				n.Flags |= FlagMount
			} else {
				sub = append(sub, job{idx, full})
			}
		} else {
			files++
			bytes += n.Size
		}
		t.Nodes = append(t.Nodes, n)
		t.Nodes[j.idx].First = idx
	}
	s.tmu.Unlock()

	s.files.Add(files)
	s.prog.Files.Add(files)
	s.prog.Bytes.Add(bytes)
	if errs > 0 {
		s.errs.Add(errs)
		s.prog.Errors.Add(errs)
	}
	return sub
}

var debugSlow = os.Getenv("FANFREE_DEBUG") != ""
