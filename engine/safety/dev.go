package safety

import (
	"strings"

	"github.com/21stware/tossy/engine/gitrepo"
	"github.com/21stware/tossy/engine/scan"
)

// DevKind is what a developer would call a piece of rebuildable content: the classes a
// developer's view of the disk is made of.
type DevKind uint8

const (
	DevNone     DevKind = iota
	DevDeps             // dependencies installed into a project (node_modules, Pods, a virtualenv)
	DevBuild            // build output and bundles inside a project (target, dist, .next)
	DevCache            // compiler and tool caches (DerivedData, go-build, ccache, .turbo)
	DevPackages         // package managers' download caches (npm, Cargo, Go modules, Gradle, Homebrew)
	DevDevices          // emulators, simulators and device support files (AVD, CoreSimulator)
	DevVMs              // container and virtual machine disks (Docker, Colima)
	DevModels           // local AI models and weights (Ollama, Hugging Face)
	DevAppCache         // application caches (~/Library/Caches, ~/.cache, Electron caches)
	devKinds
)

// DevKinds lists the kinds in the order a front end shows them when nothing else decides.
func DevKinds() []DevKind {
	out := make([]DevKind, 0, devKinds-1)
	for k := DevDeps; k < devKinds; k++ {
		out = append(out, k)
	}
	return out
}

func (k DevKind) String() string {
	return [...]string{"", "deps", "build", "cache", "packages", "devices", "vms", "models", "appcache"}[k]
}

// devOf says which kind each rule's content is, by rule ID.
var devOf = func() map[string]DevKind {
	m := map[string]DevKind{}
	for k, ids := range map[DevKind]string{
		DevDeps:     "node-modules cocoapods composer-vendor go-vendor venv py-test-envs terraform",
		DevBuild:    "cargo-target maven-target gradle-build cmake-build flutter-build swiftpm-build xcode-build dotnet-build web-framework web-dist py-dist py-egg-info pycache haskell-build zig-cache elm-stuff elixir-build unity-cache unreal-cache xcode-archives",
		DevCache:    "xcode-deriveddata xcode-previews go-buildcache compiler-cache android-build-cache web-tool-cache py-tool-cache test-coverage",
		DevPackages: "npm-cache yarn-cache pnpm-store cargo-registry cargo-git go-modcache maven-repo gradle-caches gradle-wrapper nuget-cache bun-cache pub-cache conda-pkgs ivy-cache nvm-cache homebrew-cache",
		DevDevices:  "android-avd coresimulator simulator-caches ios-devicesupport",
		DevVMs:      "docker-desktop docker-linux colima",
		DevModels:   "hf-cache torch-cache ollama-models lmstudio-models",
		DevAppCache: "user-caches xdg-cache chromium-cache inetcache",
	} {
		for _, id := range strings.Fields(ids) {
			m[id] = k
		}
	}
	return m
}()

// cacheDirKinds classes the folders of the user cache directories (~/Library/Caches, ~/.cache)
// that belong to a developer tool rather than to an app, by lowercase name. Tools with a rule
// of their own (Homebrew, go-build, Hugging Face) are classed by it.
var cacheDirKinds = func() map[string]DevKind {
	m := map[string]DevKind{}
	for k, names := range map[DevKind]string{
		DevPackages: "pip pipenv pypoetry uv yarn pnpm cocoapods org.swift.swiftpm org.carthage.carthagekit deno composer node-gyp cypress puppeteer ms-playwright electron electron-builder typescript",
		DevCache:    "bazel turbo jetbrains com.apple.dt.xcode",
	} {
		for _, n := range strings.Fields(names) {
			m[n] = k
		}
	}
	return m
}()

// Dev is the developer kind of what the rule matches (DevNone for the rest of the disk).
func (r *Rule) Dev() DevKind { return devOf[r.ID] }

// wholeOf turns a split rule into the rule one of its children carries when it stands alone.
var wholeOf = func() map[*Rule]*Rule {
	m := map[*Rule]*Rule{}
	for _, r := range builtin.all {
		if r.Split {
			m[r] = &Rule{ID: r.ID, Level: r.Level, Reason: r.Reason, Tag: r.Tag, Pick: true}
		}
	}
	return m
}()

// DevItem is one removable unit of developer content.
type DevItem struct {
	Node int32
	Rule *Rule // the rule that makes it removable; always a pick
	Kind DevKind
	Size int64
}

// DevItems lists the developer content under scope: the rule candidates up to review level that
// a developer kind covers, none inside another. Personal data is never entered, as in
// Candidates.
func (e *Evaluator) DevItems(scope int32) []DevItem {
	var out []DevItem
	for _, c := range e.Candidates(scope, Review) {
		r, k := c.Rule, c.Rule.Dev()
		if k == DevNone {
			continue
		}
		if w := wholeOf[r]; w != nil { // a child of a split folder
			r = w
			if k == DevAppCache {
				if ck, ok := cacheDirKinds[strings.ToLower(e.t.Name(c.Node))]; ok {
					k = ck
				}
			}
		}
		out = append(out, DevItem{Node: c.Node, Rule: r, Kind: k, Size: c.Size})
	}
	return out
}

// Lens returns the evaluator of a lens of e's tree (see scan.Tree.Lens; orig is its node map).
// A node of the lens is judged as its node in the scan is: rules look at the path and at what
// lies beside a folder, which only the scan knows. own gives the rules of the nodes that are
// judged otherwise: the lens's groups, and items that carry a rule of their folder's.
func (e *Evaluator) Lens(l *scan.Tree, orig []int32, own map[int32]*Rule) *Evaluator {
	le := &Evaluator{t: l, rules: e.rules, own: make([]*Rule, len(l.Nodes)), done: make([]bool, len(l.Nodes)), now: e.now,
		git: map[int32]gitrepo.Kind{}, readGit: e.readGit, base: e, orig: orig}
	for i, r := range own {
		le.own[i], le.done[i] = r, true
	}
	return le
}

// DevSkip reports whether a scan that only looks for developer content can leave the folder
// name in dir unread: nothing inside it would be among DevItems, which never enters personal
// data (Documents, Pictures, a .git) and takes a folder a rule picks as a whole, so what is in
// a trash or a temporary folder is not looked at either. A folder that may turn out to be a
// project of a personal folder's name (see namesake) is read: only its contents tell. The
// folder itself is still recorded: a .git beside a project is what makes it a checkout. It is
// for scan.Options.Skip.
func DevSkip(dir, name string) bool {
	lower := strings.ToLower(name)
	p := ""
	path := func() string { // the folder's path as rules match it
		if p == "" {
			p = strings.TrimSuffix(Norm(dir), "/") + "/" + lower
		}
		return p
	}
	for _, r := range builtin.byName[lower] {
		if r.Markers != nil {
			return false // decided by what lies beside it, which the scan has not read yet
		}
		if r.Path != nil && !r.Path.MatchString(path()) {
			continue
		}
		// r is the rule that judges the folder (Own takes the first that matches)
		switch {
		case r == builtin.personal && commonWords[lower]:
			return personalPlace.MatchString(path())
		case r.Level == Risky:
			return true
		case r.Dev() != DevNone:
			return false
		}
		// A pick of no developer kind is one item whatever it holds. Only where a path places
		// it: a folder that is merely named like a cache may be a checkout, which is entered.
		return r.Pick && r.Level <= Review && r.Path != nil
	}
	return false
}
