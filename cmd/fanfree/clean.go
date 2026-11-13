package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"time"

	"github.com/fanfree/fanfree/engine/habit"
	"github.com/fanfree/fanfree/engine/safety"
	"github.com/fanfree/fanfree/engine/scan"
	"github.com/fanfree/fanfree/engine/trash"
)

func stateDir() string {
	if d := os.Getenv("FANFREE_STATE"); d != "" {
		return d
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "FanFree")
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "fanfree:", err)
	os.Exit(1)
}

func openState() (*trash.Bin, *habit.Store) {
	b, err := trash.Open(stateDir())
	if err != nil {
		die(err)
	}
	h, err := habit.Load(filepath.Join(stateDir(), "habits.json"))
	if err != nil {
		die(err)
	}
	return b, h
}

type plan struct {
	node int32
	meta trash.Meta
	why  string
}

func cmdClean(args []string) {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	review := fs.Bool("review", false, "also take review-level rule candidates")
	yes := fs.Bool("yes", false, "actually move items to the trash")
	top := fs.Int("top", 30, "items to list")
	fs.Parse(args)
	root := "."
	if fs.NArg() > 0 {
		root = fs.Arg(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	t, err := scan.Scan(ctx, root, scan.Options{})
	if err != nil {
		die(err) // never clean from a partial tree
	}
	bin, hb := openState()
	e := safety.New(t)
	lv := safety.Safe
	if *review {
		lv = safety.Review
	}
	var ps []plan
	skip := map[int32]bool{}
	for _, c := range e.Candidates(0, lv) {
		skip[c.Node] = true
		ps = append(ps, plan{c.Node, trash.Meta{Size: c.Size, Dir: t.Nodes[c.Node].IsDir(), Source: trash.ByRule, Rule: c.Rule.ID}, c.Rule.Reason})
	}
	for _, p := range hb.Candidates(t, e, 0, skip) {
		ps = append(ps, plan{p.Node, trash.Meta{Size: p.Size, Dir: t.Nodes[p.Node].IsDir(), Source: trash.ByHabit, Habit: p.Match.Label}, fmt.Sprintf("习惯：%s（已手动删除 %d 次）", p.Match.Label, p.Match.D)})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].meta.Size > ps[j].meta.Size })
	var total int64
	for _, p := range ps {
		total += p.meta.Size
	}
	fmt.Printf("%d items · %s\n\n", len(ps), size(total))
	for i, p := range ps {
		if i < *top {
			fmt.Printf("  %10s  %-6s %s\n              %s\n", size(p.meta.Size), p.meta.Source, t.Path(p.node), p.why)
		}
	}
	if !*yes {
		fmt.Println("\ndry run — add -yes to move these to the trash (restorable with `fanfree trash restore`)")
		return
	}
	var moved, freed int64
	var failed int
	t0 := time.Now()
	for _, p := range ps {
		if _, err := bin.Move(t.Path(p.node), p.meta); err != nil {
			failed++
			fmt.Fprintln(os.Stderr, "  skip:", err)
			continue
		}
		moved++
		freed += p.meta.Size
	}
	fmt.Printf("\nmoved %d items · %s to the trash in %d ms", moved, size(freed), time.Since(t0).Milliseconds())
	if failed > 0 {
		fmt.Printf(" · %d skipped", failed)
	}
	fmt.Println()
}

func cmdRm(args []string) {
	if len(args) == 0 {
		usage()
	}
	bin, hb := openState()
	for _, a := range args {
		abs, err := filepath.Abs(a)
		if err != nil {
			die(err)
		}
		fi, err := os.Lstat(abs)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		sz := fi.Size()
		if fi.IsDir() {
			if st, err := scan.Scan(context.Background(), abs, scan.Options{}); err == nil {
				sz = st.Stats.Bytes
			}
		}
		if _, err := bin.Move(abs, trash.Meta{Size: sz, Dir: fi.IsDir(), Source: trash.ByManual}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			continue
		}
		fmt.Printf("trashed %s (%s)\n", abs, size(sz))
		age := int(time.Since(fi.ModTime()).Hours() / 24)
		if l := hb.Learn(habit.Subject{Path: abs, Dir: fi.IsDir(), Size: sz, AgeDays: age}, 1, 0); l != "" {
			fmt.Printf("  learned: 以后自动清理会包含「%s」\n", l)
		}
	}
	if err := hb.Save(); err != nil {
		die(err)
	}
}

func cmdTrash(args []string) {
	bin, hb := openState()
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list":
		for _, it := range bin.List() {
			why := it.Rule
			if it.Source == trash.ByHabit {
				why = it.Habit
			}
			fmt.Printf("%s  %10s  %s  %-6s %-16s %s\n", it.ID, size(it.Size), it.When.Format("01-02 15:04"), it.Source, why, it.Orig)
		}
		fmt.Printf("\n%s in the trash from FanFree\n", size(bin.Size()))
	case "restore":
		for _, id := range args {
			it, err := bin.Restore(id)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			fmt.Println("restored", it.Orig)
			sub := habit.Subject{Path: it.Orig, Dir: it.Dir, Size: it.Size}
			if fi, err := os.Lstat(it.Orig); err == nil {
				sub.AgeDays = int(time.Since(fi.ModTime()).Hours() / 24)
			}
			switch it.Source {
			case trash.ByHabit: // the habit was wrong
				if l := hb.Learn(sub, 0, 1); l != "" {
					fmt.Printf("  habit off: 不再自动清理「%s」\n", l)
				}
			case trash.ByManual: // undo of a manual delete
				hb.Learn(sub, -1, 0)
			}
		}
		if err := hb.Save(); err != nil {
			die(err)
		}
	case "empty":
		var ids []string
		if len(args) > 0 {
			ids = args
		}
		freed, err := bin.Empty(ids)
		fmt.Printf("freed %s\n", size(freed))
		if err != nil {
			die(err)
		}
	default:
		usage()
	}
}

func cmdHabit(args []string) {
	_, hb := openState()
	if len(args) >= 2 && args[0] == "forget" {
		hb.Forget(args[1])
		if err := hb.Save(); err != nil {
			die(err)
		}
		return
	}
	es := hb.Entries()
	keys := make([]string, 0, len(es))
	for k := range es {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		e := es[k]
		on := " "
		if float64(e.D-2*e.R)*e.W >= 2 {
			on = "●"
		}
		fmt.Printf("%s d=%d r=%d max=%-9s %s\n    %s\n", on, e.D, e.R, size(e.Max), e.Label, k)
	}
}
