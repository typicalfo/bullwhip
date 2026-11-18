package bw

import "runtime/debug"

// Go embeds the source revision when building from a Git checkout. Read the
// binary's metadata, never the repository where a worker happens to run it.
func buildVersion() string {
	revision, modified := "", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if revision == "" {
		return "bw unknown (build commit unavailable)"
	}
	if modified {
		return "bw " + revision + " (modified)"
	}
	return "bw " + revision
}
