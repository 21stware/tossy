// Command tossy is the engine's command-line front end: scan, evaluate, clean.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"time"

	"github.com/21stware/tossy/engine/safety"
	"github.com/21stware/tossy/engine/scan"
)

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  tossy scan [-w workers] [-x] [-top n] [path]   scan and print the largest entries
  tossy eval [-review] [-top n] [path]           scan, then list what is safe to clean (read-only)
  tossy dev [-top n] [path]                      scan, then list developer content by kind (read-only)
  tossy clean [-review] [-yes] [path]            move safe items (+ learned habits) to the trash; dry run without -yes
  tossy rm path...                               move to the trash by hand (Tossy learns from this)
  tossy trash [list | restore id... | empty [id...]]
  tossy habit [list | forget key]`)
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "scan":
		cmdScan(os.Args[2:])
	case "eval":
		cmdEval(os.Args[2:])
	case "dev":
		cmdDev(os.Args[2:])
	case "clean":
		cmdClean(os.Args[2:])
	case "rm":
		cmdRm(os.Args[2:])
	case "trash":
		cmdTrash(os.Args[2:])
	case "habit":
		cmdHabit(os.Args[2:])
	default:
		usage()
	}
}

func cmdScan(args []string) {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	workers := fs.Int("w", 0, "worker goroutines (0 = auto)")
	cross := fs.Bool("x", false, "cross filesystem boundaries")
	top := fs.Int("top", 12, "entries to list")
	quiet := fs.Bool("q", false, "no live progress")
	fs.Parse(args)
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	prog := &scan.Progress{}
	done := make(chan struct{})
	if !*quiet {
		go func() {
			tk := time.NewTicker(150 * time.Millisecond)
			defer tk.Stop()
			for {
				select {
				case <-done:
					fmt.Fprint(os.Stderr, "\r\033[K")
					return
				case <-tk.C:
					fmt.Fprintf(os.Stderr, "\r\033[K%s files  %s  %.60s", human(prog.Files.Load()), size(prog.Bytes.Load()), prog.Current())
				}
			}
		}()
	}
	t, err := scan.Scan(ctx, root, scan.Options{Workers: *workers, CrossDevice: *cross, Progress: prog})
	close(done)
	if t == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "partial:", err)
	}
	var ms runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&ms)
	s := t.Stats
	fmt.Printf("%s  %s files, %s dirs, %s nodes, %d errors, %d hardlinks\n", t.Root, human(s.Files), human(s.Dirs), human(int64(len(t.Nodes))), s.Errors, s.Hardlinks)
	fmt.Printf("%s on disk · %d ms · %.0f k entries/s · heap %s\n\n", size(s.Bytes), s.Millis, float64(len(t.Nodes))/float64(max(s.Millis, 1)), size(int64(ms.HeapAlloc)))
	for i, c := range t.Children(0, nil) {
		if i >= *top {
			break
		}
		n := &t.Nodes[c]
		kind := " "
		if n.IsDir() {
			kind = "/"
		}
		fmt.Printf("  %10s  %5.1f%%  %s%s\n", size(n.Size), 100*float64(n.Size)/float64(max(s.Bytes, 1)), t.Name(c), kind)
	}
}

func size(b int64) string {
	f := float64(b)
	for _, u := range []string{"B", "KB", "MB", "GB", "TB"} {
		if f < 1000 || u == "TB" {
			if u == "B" {
				return fmt.Sprintf("%d B", b)
			}
			return fmt.Sprintf("%.1f %s", f, u)
		}
		f /= 1024
	}
	return ""
}

func human(n int64) string {
	switch {
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1e4:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

func cmdEval(args []string) {
	fs := flag.NewFlagSet("eval", flag.ExitOnError)
	review := fs.Bool("review", false, "include review-level candidates")
	top := fs.Int("top", 25, "candidates to list")
	fs.Parse(args)
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	t, err := scan.Scan(ctx, root, scan.Options{})
	if t == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	t0 := time.Now()
	e := safety.New(t)
	max := safety.Safe
	if *review {
		max = safety.Review
	}
	cs := e.Candidates(0, max)
	sums := e.LevelSums(0)
	el := time.Since(t0)
	sort.Slice(cs, func(i, j int) bool { return cs[i].Size > cs[j].Size })
	var total int64
	for _, c := range cs {
		total += c.Size
	}
	fmt.Printf("%s · %s scanned in %d ms · evaluated in %d ms\n", t.Root, size(t.Stats.Bytes), t.Stats.Millis, el.Milliseconds())
	fmt.Printf("safe %s · review %s · personal %s · protected %s\n", size(sums[0]), size(sums[1]), size(sums[2]), size(sums[3]))
	fmt.Printf("\n%d candidates · %s reclaimable\n\n", len(cs), size(total))
	for i, c := range cs {
		if i >= *top {
			break
		}
		fmt.Printf("  %10s  %-7s %-6s %s\n", size(c.Size), c.Rule.Level, c.Rule.Tag, t.Path(c.Node))
	}
}

// devKindNames are the developer kinds as the command line prints them.
var devKindNames = map[safety.DevKind]string{
	safety.DevDeps: "Dependencies", safety.DevBuild: "Build output", safety.DevCache: "Build caches",
	safety.DevPackages: "Package caches", safety.DevDevices: "Emulators & devices", safety.DevVMs: "Containers & VMs",
	safety.DevModels: "Local models", safety.DevAppCache: "App caches",
}

// cmdDev is the developer's view on the command line: what building software left behind, by
// kind, largest first.
func cmdDev(args []string) {
	fs := flag.NewFlagSet("dev", flag.ExitOnError)
	top := fs.Int("top", 5, "items to list per kind")
	fs.Parse(args)
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// personal data and trash folders hold nothing this lists: they are not read at all
	t, err := scan.Scan(ctx, root, scan.Options{Skip: safety.DevSkip})
	if t == nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	t0 := time.Now()
	e := safety.New(t)
	type group struct {
		kind  safety.DevKind
		size  int64
		items []safety.DevItem
	}
	groups := map[safety.DevKind]*group{}
	var total, safe int64
	for _, it := range e.DevItems(0) {
		g := groups[it.Kind]
		if g == nil {
			g = &group{kind: it.Kind}
			groups[it.Kind] = g
		}
		g.items = append(g.items, it)
		g.size += it.Size
		total += it.Size
		if it.Rule.Level == safety.Safe {
			safe += it.Size
		}
	}
	el := time.Since(t0)
	var gs []*group
	for _, g := range groups {
		sort.Slice(g.items, func(i, j int) bool { return g.items[i].Size > g.items[j].Size })
		gs = append(gs, g)
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i].size > gs[j].size })
	fmt.Printf("%s · scanned in %d ms · evaluated in %d ms\n", t.Root, t.Stats.Millis, el.Milliseconds())
	fmt.Printf("developer content %s · %s of it safe to clean\n", size(total), size(safe))
	for _, g := range gs {
		fmt.Printf("\n%-20s %10s  %d items\n", devKindNames[g.kind], size(g.size), len(g.items))
		for i, it := range g.items {
			if i >= *top {
				fmt.Printf("  %10s  %d more\n", "", len(g.items)-i)
				break
			}
			fmt.Printf("  %10s  %-7s %s\n", size(it.Size), it.Rule.Level, t.Path(it.Node))
		}
	}
}
