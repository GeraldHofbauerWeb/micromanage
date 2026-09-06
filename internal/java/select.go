package java

import (
	"context"
	"fmt"
	"strings"
)

// Requirement describes the runtime a launch needs.
type Requirement struct {
	// Major is the version's javaVersion.majorVersion. Zero means unknown.
	Major int
	// Component is the version's javaVersion.component, used to prefer an
	// exactly matching Mojang runtime and to know what to download.
	Component string
	// Strict refuses a newer major release. Forge 47.x on Java 21 is a known
	// broken combination, so a loader that cares sets this rather than
	// letting the launcher silently upgrade.
	Strict bool
}

// Selection is a chosen runtime plus anything the user should be told.
type Selection struct {
	Runtime Runtime
	Warning string
}

// Select picks the best runtime for a requirement.
//
// An exact major match always wins. A newer major is accepted only with a
// warning and only when the requirement is not strict; an older one never is,
// because the game simply will not run.
func Select(runtimes []Runtime, req Requirement) (Selection, error) {
	usable := make([]Runtime, 0, len(runtimes))
	var broken []Runtime
	for _, r := range runtimes {
		if r.Broken {
			broken = append(broken, r)
			continue
		}
		usable = append(usable, r)
	}

	if len(usable) == 0 {
		return Selection{}, noRuntimeError(req, broken)
	}

	if req.Major == 0 {
		// Nothing to match against; the newest usable runtime is the best
		// guess, and Detect already sorted them.
		return Selection{Runtime: usable[0]}, nil
	}

	// Exact major match, preferring the runtime whose component the version
	// actually names.
	var exact []Runtime
	for _, r := range usable {
		if r.Major == req.Major {
			exact = append(exact, r)
		}
	}
	if len(exact) > 0 {
		for _, r := range exact {
			if req.Component != "" && r.Component == req.Component {
				return Selection{Runtime: r}, nil
			}
		}
		return Selection{Runtime: exact[0]}, nil
	}

	if req.Strict {
		return Selection{}, fmt.Errorf(
			"this version needs Java %d and no Java %d runtime is installed; "+
				"a newer release is not a safe substitute here", req.Major, req.Major)
	}

	// Fall back to the closest newer release, with a warning.
	var best *Runtime
	for i := range usable {
		if usable[i].Major < req.Major {
			continue
		}
		if best == nil || usable[i].Major < best.Major {
			best = &usable[i]
		}
	}
	if best == nil {
		return Selection{}, fmt.Errorf(
			"this version needs Java %d but only older runtimes were found", req.Major)
	}

	return Selection{
		Runtime: *best,
		Warning: fmt.Sprintf(
			"this version asks for Java %d but only Java %d was available; mods may misbehave",
			req.Major, best.Major),
	}, nil
}

// noRuntimeError explains an empty selection, pointing at repairable runtimes
// when there are any.
func noRuntimeError(req Requirement, broken []Runtime) error {
	if len(broken) > 0 {
		var details []string
		for _, r := range broken {
			details = append(details, fmt.Sprintf("%s (%s)", r.Path, r.Reason))
		}
		return fmt.Errorf(
			"no usable Java runtime found, but %d unusable one(s) are present:\n  %s\n"+
				"run 'repair-perms' if they are only missing their executable bit",
			len(broken), strings.Join(details, "\n  "))
	}
	if req.Major > 0 {
		return fmt.Errorf("no Java runtime found; this version needs Java %d", req.Major)
	}
	return fmt.Errorf("no Java runtime found")
}

// Resolve is the whole path from a requirement to a usable executable,
// honouring an explicit override.
func (d *Detector) Resolve(ctx context.Context, req Requirement, overridePath string) (Selection, error) {
	if overridePath != "" {
		rt := probeRuntime(ctx, probe{path: overridePath, source: "configured"})
		if rt.Path == "" {
			return Selection{}, fmt.Errorf("configured Java path %q does not exist", overridePath)
		}
		if rt.Broken {
			return Selection{}, fmt.Errorf("configured Java at %q is unusable: %s", overridePath, rt.Reason)
		}

		sel := Selection{Runtime: rt}
		// An explicit choice is honoured, but a mismatch is still worth
		// saying out loud.
		if req.Major > 0 && rt.Major != req.Major {
			sel.Warning = fmt.Sprintf(
				"configured Java is %d but this version asks for %d", rt.Major, req.Major)
		}
		return sel, nil
	}

	return Select(d.Detect(ctx), req)
}

// RequirementFor derives a requirement from a version's javaVersion block and
// the loader in use.
func RequirementFor(component string, major int, strict bool) Requirement {
	return Requirement{Major: major, Component: component, Strict: strict}
}
