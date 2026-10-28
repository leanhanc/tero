// Package build turns an app's source into an image in tero-agent's Podman store:
// Railpack plans the build, a throwaway rootless BuildKit container runs it,
// and the result is loaded with `podman load`. No registry is involved.
package build

// Failure is a build that failed for a reason the admin can act on, such as
// an app Railpack can't plan or a failing build step. Any other error from
// this package is Tero's own.
type Failure struct {
	Reason string
}

func (f Failure) Error() string {
	return f.Reason
}

var (
	errNoStartCommand = Failure{Reason: `Railpack couldn't find how to start your app. Add a "start" script to package.json, a "main" field, or an index.js or index.ts file`}
	errTimedOut       = Failure{Reason: "The build took longer than 30 minutes and was stopped"}
	errOutOfMemory    = Failure{Reason: "The build ran out of memory. It can use up to half of this server's memory"}
	errBuildFailed    = Failure{Reason: "A build step failed. The build log above shows which one and why"}
)
