package main

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/21stware/tossy/engine/trash"
)

// Developer aids for looking at the empty-trash vehicles (truck.go) without a disk full of
// trash. Unset, the app behaves as shipped.
//
//   - TOSSY_PREVIEW=1 plays emptying with the deletion simulated: the vehicle comes, takes the
//     items as if they went, and leaves, and nothing is deleted, so the bin comes back full to
//     play again. TOSSY_PREVIEW=van, ev or skiff also sends that vehicle, whatever the bin.
//   - TOSSY_DISK pretends the volume is that size (20GB, 500MB, 1.5TB, or bytes), as full as the
//     real one. The bin's size, and so the vehicle, goes by the trash's share of the disk: on a
//     small pretend disk a few files fill a skip.

var (
	previewOn   bool
	previewKind vehicle = -1
)

func init() {
	switch v := strings.ToLower(strings.TrimSpace(os.Getenv("TOSSY_PREVIEW"))); v {
	case "":
	case "van":
		previewOn, previewKind = true, vehVan
	case "ev":
		previewOn, previewKind = true, vehEV
	case "skiff":
		previewOn, previewKind = true, vehSkiff
	default:
		previewOn = true
	}
	if total := parseSize(os.Getenv("TOSSY_DISK")); total > 0 {
		real := diskUsageOf
		diskUsageOf = func(path string) (int64, int64, bool) {
			t, f, ok := real(path)
			free := total / 2
			if ok && t > 0 {
				free = int64(float64(total) * float64(f) / float64(t))
			}
			return total, free, true
		}
	}
}

// simulate stands in for EmptyWith under TOSSY_PREVIEW (from its own goroutine): each item
// "goes" after a while in proportion to its size, about 3s for all, and nothing is deleted.
func (r *truckRun) simulate(items, left []trash.Item) {
	var total int64
	for _, it := range items {
		total += it.Size
	}
	const budget = 3 * time.Second
	n := float64(max(1, len(items)))
	for _, it := range items {
		r.step(trash.Step{Item: it})
		share := .3/n + .7*float64(it.Size)/float64(max(1, total))
		time.Sleep(max(60*time.Millisecond, time.Duration(float64(budget)*share)))
		r.step(trash.Step{Item: it, Done: true})
	}
	r.finish(total, left, nil)
}

// previewUnmark ends a preview as a real empty ends, with the items gone from the bin, by only
// dropping their marks: the files never moved, so they reappear in the chart and Auto Clean can
// take them again to play it once more. An item an older version really moved stays in the
// bin (putting it back would move files). It returns what is left in the bin.
func (a *app) previewUnmark(r *truckRun) []trash.Item {
	for _, e := range r.ents {
		if _, ok := r.sizes[e.key]; !ok || e.id == "" {
			continue
		}
		var it *trash.Item
		for _, x := range a.bin.List() {
			if x.ID == e.id {
				it = &x
				break
			}
		}
		if it == nil || it.Stored != it.Orig { // not a mark
			continue
		}
		if _, err := a.bin.Restore(e.id); err != nil {
			continue
		}
		a.putBack(e.id)
	}
	a.syncLens()
	return a.bin.List()
}

// parseSize reads "20GB", "500 MB", "1.5TB", "300k" or a byte count (binary units); 0 if it
// cannot.
func parseSize(s string) int64 {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	mult := 1.0
	for _, u := range []struct {
		suffix string
		m      float64
	}{{"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10}, {"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10}, {"B", 1}} {
		if strings.HasSuffix(s, u.suffix) {
			s, mult = strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), u.m
			break
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return int64(v * mult)
}
